import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "@/api/client";
import { FolderOpen, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useConfirm } from "@beacon-shared/ConfirmDialog";

interface Category {
  name: string;
  save_path: string;
  upload_limit: number;
  download_limit: number;
}

function useCategories() {
  return useQuery({
    queryKey: ["categories"],
    queryFn: () => apiFetch<Category[]>("/categories"),
  });
}

function useCreateCategory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (cat: { name: string; save_path?: string }) =>
      apiFetch<Category>("/categories", { method: "POST", body: JSON.stringify(cat) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["categories"] }),
  });
}

function useDeleteCategory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) =>
      apiFetch(`/categories/${encodeURIComponent(name)}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["categories"] }),
  });
}

export default function CategoriesPage() {
  const { data: categories, isLoading } = useCategories();
  const createCategory = useCreateCategory();
  const deleteCategory = useDeleteCategory();
  const confirm = useConfirm();
  const [name, setName] = useState("");
  const [savePath, setSavePath] = useState("");

  async function handleDelete(catName: string) {
    if (
      await confirm({
        title: "Delete category",
        message: `Delete category "${catName}"? Torrents in this category keep their tag but lose the association.`,
        confirmLabel: "Delete",
      })
    ) {
      deleteCategory.mutate(catName, { onSuccess: () => toast.success(`Deleted: ${catName}`) });
    }
  }

  function handleCreate() {
    if (!name.trim()) return;
    createCategory.mutate(
      { name: name.trim(), save_path: savePath.trim() || undefined },
      {
        onSuccess: () => {
          toast.success(`Created category: ${name}`);
          setName("");
          setSavePath("");
        },
        onError: (e) => toast.error((e as Error).message),
      }
    );
  }

  return (
    <div style={{ padding: 24, maxWidth: 700, margin: "0 auto" }}>
      <div style={{ marginBottom: 24 }}>
        <h1 style={{ fontSize: 20, fontWeight: 600, color: "var(--color-text-primary)", margin: 0 }}>Categories</h1>
        <p style={{ fontSize: 13, color: "var(--color-text-secondary)", margin: "4px 0 0" }}>
          Organize torrents with categories and assign default save paths.
        </p>
      </div>

      {/* Create form */}
      <div style={{
        background: "var(--color-bg-surface)",
        border: "1px solid var(--color-border-subtle)",
        borderRadius: 10,
        padding: 20,
        marginBottom: 20,
      }}>
        <div style={{ display: "flex", gap: 10, alignItems: "flex-end" }}>
          <div style={{ flex: 1 }}>
            <label style={{ display: "block", fontSize: 12, fontWeight: 500, color: "var(--color-text-secondary)", marginBottom: 4 }}>Name</label>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. movies"
              onKeyDown={(e) => e.key === "Enter" && handleCreate()}
              style={{
                width: "100%",
                padding: "8px 12px",
                borderRadius: 6,
                border: "1px solid var(--color-border-default)",
                background: "var(--color-bg-elevated)",
                color: "var(--color-text-primary)",
                fontSize: 13,
                outline: "none",
              }}
            />
          </div>
          <div style={{ flex: 2 }}>
            <label style={{ display: "block", fontSize: 12, fontWeight: 500, color: "var(--color-text-secondary)", marginBottom: 4 }}>Save Path (optional)</label>
            <input
              value={savePath}
              onChange={(e) => setSavePath(e.target.value)}
              placeholder="/downloads/movies"
              onKeyDown={(e) => e.key === "Enter" && handleCreate()}
              style={{
                width: "100%",
                padding: "8px 12px",
                borderRadius: 6,
                border: "1px solid var(--color-border-default)",
                background: "var(--color-bg-elevated)",
                color: "var(--color-text-primary)",
                fontSize: 13,
                outline: "none",
              }}
            />
          </div>
          <button
            onClick={handleCreate}
            disabled={!name.trim() || createCategory.isPending}
            style={{
              display: "flex",
              alignItems: "center",
              gap: 6,
              padding: "8px 14px",
              borderRadius: 6,
              border: "none",
              background: name.trim() ? "var(--color-accent)" : "var(--color-bg-subtle)",
              color: name.trim() ? "var(--color-accent-fg)" : "var(--color-text-muted)",
              fontSize: 13,
              fontWeight: 500,
              cursor: name.trim() ? "pointer" : "not-allowed",
              flexShrink: 0,
            }}
          >
            <Plus size={14} /> Add
          </button>
        </div>
      </div>

      {/* Loading */}
      {isLoading && (
        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          {[1, 2].map((i) => <div key={i} className="skeleton" style={{ height: 52, borderRadius: 6 }} />)}
        </div>
      )}

      {/* Empty state */}
      {categories && categories.length === 0 && (
        <div style={{ textAlign: "center", padding: "48px 0" }}>
          <FolderOpen size={32} style={{ color: "var(--color-text-muted)", marginBottom: 12 }} />
          <p style={{ fontSize: 14, color: "var(--color-text-secondary)", fontWeight: 500 }}>No categories</p>
          <p style={{ fontSize: 13, color: "var(--color-text-muted)", margin: "6px 0 0" }}>Create one above to start organizing your torrents.</p>
        </div>
      )}

      {/* Category list */}
      {categories && categories.length > 0 && (
        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          {categories.map((cat) => (
            <div
              key={cat.name}
              style={{
                display: "flex",
                alignItems: "center",
                gap: 12,
                padding: "12px 16px",
                background: "var(--color-bg-surface)",
                border: "1px solid var(--color-border-subtle)",
                borderRadius: 6,
              }}
            >
              <FolderOpen size={15} style={{ color: "var(--color-accent)", flexShrink: 0 }} />
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontSize: 14, fontWeight: 500, color: "var(--color-text-primary)" }}>{cat.name}</div>
                {cat.save_path && (
                  <div style={{ fontSize: 12, color: "var(--color-text-muted)", fontFamily: "var(--font-family-mono)", marginTop: 2 }}>
                    {cat.save_path}
                  </div>
                )}
              </div>
              <button
                onClick={() => handleDelete(cat.name)}
                style={{
                  background: "transparent",
                  border: "none",
                  color: "var(--color-text-muted)",
                  cursor: "pointer",
                  padding: 4,
                  borderRadius: 4,
                  display: "flex",
                  alignItems: "center",
                }}
              >
                <Trash2 size={14} />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
