package torrent

// session_remove_test.go — Regression guard for the "DB row orphaned when
// anacrolix Drop() panics" bug.
//
// Real-world incident (John Wick, info hash ec5086c1c…):
//
//  1. User clicks Remove in the UI.
//  2. Session.Remove deletes the in-memory entry, then calls mt.t.Drop().
//  3. anacrolix's tracker-announcer goroutine fires a panicif.False
//     assertion mid-Drop. The panic unwinds back through Drop().
//  4. Without the defer, s.deleteTorrent(hash) was the line AFTER Drop —
//     it never ran, so the row stayed in Postgres.
//  5. On next container restart, restoreFromDB resurrects the same
//     torrent, which immediately re-triggers the same panic. Permanent
//     crashloop until the row is manually deleted.
//
// The fix in Session.Remove is a one-liner: move the deleteTorrent call
// into a `defer` BEFORE Drop() is invoked. That guarantees DB cleanup
// happens even if Drop() panics.

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestRemove_RemovesFromInMemoryMap is the baseline behavioral check:
// after Remove() returns, the torrent is gone from s.torrents and a
// subsequent Get() reports "not found". Locks down the happy path so
// future refactors can't accidentally regress the simplest case.
func TestRemove_RemovesFromInMemoryMap(t *testing.T) {
	s := newTestSession(t)
	hash, _ := addTestTorrent(t, s, nil)

	// Sanity: torrent is present before Remove.
	s.mu.RLock()
	_, present := s.torrents[hash]
	s.mu.RUnlock()
	if !present {
		t.Fatal("torrent missing from session map before Remove — addTestTorrent broken?")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Remove(ctx, hash, false); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}

	s.mu.RLock()
	_, stillPresent := s.torrents[hash]
	s.mu.RUnlock()
	if stillPresent {
		t.Error("torrent still in session map after Remove — in-memory cleanup regressed")
	}

	if _, err := s.Get(hash); err == nil {
		t.Error("Get returned no error for removed torrent — expected 'not found'")
	}
}

// TestRemove_UnknownHashReturnsError is the negative case. A bare
// "torrent not found" error keeps the HTTP layer's 404 mapping clean —
// the same shape Get/Pause/Resume use.
func TestRemove_UnknownHashReturnsError(t *testing.T) {
	s := newTestSession(t)
	err := s.Remove(context.Background(), "0000000000000000000000000000000000000000", false)
	if err == nil {
		t.Fatal("expected error removing unknown hash, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should say 'not found', got: %v", err)
	}
}

// TestRemove_DeleteTorrentRunsInDefer is a *source-shape* regression
// guard. The behavioral test above can't simulate an anacrolix-internal
// panic during Drop() without invasive refactoring, so we instead lock
// down the source code: in Session.Remove, the call to s.deleteTorrent
// MUST appear in a `defer` statement, and that defer MUST be registered
// BEFORE mt.t.Drop() is invoked. If anyone moves deleteTorrent back to
// running inline after Drop(), this test fails loudly and explains why.
//
// This is the only way to keep the John Wick crashloop from regressing
// without standing up a full sqlmock + panic-injection harness.
func TestRemove_DeleteTorrentRunsInDefer(t *testing.T) {
	// Resolve session.go relative to the running test binary so the
	// test works under both `go test ./...` and IDE runners.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate session.go")
	}
	sessionGo := filepath.Join(filepath.Dir(thisFile), "session.go")

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, sessionGo, nil, 0)
	if err != nil {
		t.Fatalf("parse session.go: %v", err)
	}

	var removeFn *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Name.Name != "Remove" {
			continue
		}
		// Method on *Session — confirm the receiver type.
		if fn.Recv == nil || len(fn.Recv.List) != 1 {
			continue
		}
		star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		ident, ok := star.X.(*ast.Ident)
		if !ok || ident.Name != "Session" {
			continue
		}
		removeFn = fn
		break
	}
	if removeFn == nil {
		t.Fatal("could not find (*Session).Remove in session.go — was it renamed?")
	}

	// Walk the body in order. We want to see a `defer s.deleteTorrent(...)`
	// statement BEFORE the `mt.t.Drop()` call. Anything else is a regression.
	deferIdx, dropIdx := -1, -1
	for i, stmt := range removeFn.Body.List {
		if isDeferDeleteTorrent(stmt) {
			if deferIdx < 0 {
				deferIdx = i
			}
		}
		if isMtDropCall(stmt) {
			if dropIdx < 0 {
				dropIdx = i
			}
		}
	}

	if deferIdx < 0 {
		t.Fatal(
			"REGRESSION: (*Session).Remove no longer registers a `defer s.deleteTorrent(hash)`. " +
				"That defer is the only thing keeping the John Wick crashloop fixed — " +
				"without it, an anacrolix panic during Drop() leaves an orphan torrents row " +
				"that restoreFromDB resurrects on every restart. See session.go comment above Remove().",
		)
	}
	if dropIdx < 0 {
		t.Fatal("could not find `mt.t.Drop()` call in (*Session).Remove — file shape changed unexpectedly")
	}
	if deferIdx >= dropIdx {
		t.Fatalf(
			"REGRESSION: `defer s.deleteTorrent(hash)` appears at body index %d, AFTER `mt.t.Drop()` at index %d. "+
				"Defers must be REGISTERED before Drop runs — otherwise a Drop panic skips the cleanup. "+
				"Move the defer above the Drop call in (*Session).Remove.",
			deferIdx, dropIdx,
		)
	}
}

// isDeferDeleteTorrent reports whether a statement is `defer s.deleteTorrent(...)`.
func isDeferDeleteTorrent(stmt ast.Stmt) bool {
	def, ok := stmt.(*ast.DeferStmt)
	if !ok {
		return false
	}
	sel, ok := def.Call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return sel.Sel.Name == "deleteTorrent"
}

// isMtDropCall reports whether a statement is `mt.t.Drop()`.
func isMtDropCall(stmt ast.Stmt) bool {
	expr, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := expr.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Drop" {
		return false
	}
	// Receiver of Drop should be `mt.t`.
	inner, ok := sel.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if inner.Sel.Name != "t" {
		return false
	}
	mt, ok := inner.X.(*ast.Ident)
	return ok && mt.Name == "mt"
}
