import { useState, useEffect, useMemo } from "react";
import { Link } from "react-router-dom";
import {
  Activity,
  Search,
  ArrowUp,
  ArrowDown,
  ChevronLeft,
  ChevronRight,
  CheckCircle,
  Trash2,
  Download,
} from "lucide-react";
import {
  useActivityList,
  type ActivityItem,
  type ActivitySort,
  type ActivityStatus,
} from "@/api/activity";
import { formatBytes, timeAgo } from "@/shared/utils";

const PAGE_SIZE = 50;

const STATUS_TABS: { key: ActivityStatus; label: string }[] = [
  { key: "all", label: "All" },
  { key: "active", label: "Active" },
  { key: "completed", label: "Completed" },
  { key: "removed", label: "Removed" },
];

interface SortableColumn {
  key: ActivitySort;
  label: string;
  align?: "left" | "right";
  width?: number | string;
}

const COLUMNS: SortableColumn[] = [
  { key: "name", label: "Name", align: "left" },
  { key: "size_bytes", label: "Size", align: "right", width: 90 },
  { key: "resolution", label: "Quality", align: "left", width: 80 },
  { key: "added_at", label: "Added", align: "left", width: 100 },
  { key: "completed_at", label: "Status", align: "left", width: 110 },
];

export default function ActivityPage() {
  // Search input — debounced into the query so each keystroke
  // doesn't fire an HTTP request, but the list still updates "as you
  // type" within ~250ms.
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  useEffect(() => {
    const t = setTimeout(() => setSearch(searchInput.trim()), 250);
    return () => clearTimeout(t);
  }, [searchInput]);

  const [status, setStatus] = useState<ActivityStatus>("all");
  const [sort, setSort] = useState<ActivitySort>("added_at");
  const [order, setOrder] = useState<"asc" | "desc">("desc");
  const [offset, setOffset] = useState(0);

  // Reset pagination when filters change. Keeping the user on page 7
  // after they typed a fresh query gives them an empty page that
  // looks like a bug.
  useEffect(() => {
    setOffset(0);
  }, [search, status, sort, order]);

  const { data, isLoading, error } = useActivityList({
    q: search,
    status,
    sort,
    order,
    limit: PAGE_SIZE,
    offset,
  });

  const items = data?.items ?? [];
  const total = data?.total ?? 0;
  const page = Math.floor(offset / PAGE_SIZE) + 1;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const onHeaderClick = (col: ActivitySort) => {
    if (sort === col) {
      setOrder((o) => (o === "asc" ? "desc" : "asc"));
    } else {
      setSort(col);
      setOrder("desc");
    }
  };

  return (
    <div style={{ padding: "24px 32px", maxWidth: 1400, margin: "0 auto" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "baseline", marginBottom: 16 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 22, fontWeight: 600, color: "var(--color-text-primary)" }}>
            Activity
          </h1>
          <p style={{ margin: "4px 0 0", fontSize: 13, color: "var(--color-text-secondary)" }}>
            Every torrent Haul has handled — active, completed, and removed.
            {total > 0 && (
              <>
                {" "}
                <span style={{ color: "var(--color-text-muted)" }}>
                  {total.toLocaleString()} total
                </span>
              </>
            )}
          </p>
        </div>
      </div>

      {/* Filter row */}
      <div
        style={{
          display: "flex",
          gap: 12,
          marginBottom: 12,
          alignItems: "center",
          flexWrap: "wrap",
        }}
      >
        <div style={{ position: "relative", flex: "1 1 280px", maxWidth: 400 }}>
          <Search
            size={14}
            style={{
              position: "absolute",
              left: 10,
              top: "50%",
              transform: "translateY(-50%)",
              color: "var(--color-text-muted)",
              pointerEvents: "none",
            }}
          />
          <input
            type="search"
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            placeholder="Search name or category…"
            style={{
              width: "100%",
              padding: "8px 12px 8px 32px",
              borderRadius: 6,
              border: "1px solid var(--color-border-default)",
              background: "var(--color-bg-surface)",
              color: "var(--color-text-primary)",
              fontSize: 13,
              outline: "none",
            }}
          />
        </div>

        <div style={{ display: "flex", gap: 4 }}>
          {STATUS_TABS.map((t) => {
            const active = status === t.key;
            return (
              <button
                key={t.key}
                onClick={() => setStatus(t.key)}
                style={{
                  padding: "6px 12px",
                  fontSize: 12,
                  fontWeight: 500,
                  borderRadius: 6,
                  border: `1px solid ${active ? "var(--color-accent)" : "var(--color-border-default)"}`,
                  background: active ? "var(--color-accent)" : "var(--color-bg-surface)",
                  color: active ? "#fff" : "var(--color-text-secondary)",
                  cursor: "pointer",
                }}
              >
                {t.label}
              </button>
            );
          })}
        </div>
      </div>

      {/* Table */}
      <div
        style={{
          background: "var(--color-bg-surface)",
          border: "1px solid var(--color-border-subtle)",
          borderRadius: 8,
          overflow: "hidden",
        }}
      >
        <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 13 }}>
          <thead>
            <tr style={{ background: "var(--color-bg-elevated)" }}>
              {COLUMNS.map((c) => (
                <th
                  key={c.key}
                  onClick={() => onHeaderClick(c.key)}
                  style={{
                    padding: "10px 14px",
                    textAlign: c.align ?? "left",
                    fontSize: 11,
                    fontWeight: 600,
                    textTransform: "uppercase",
                    letterSpacing: 0.4,
                    color: "var(--color-text-muted)",
                    cursor: "pointer",
                    userSelect: "none",
                    width: c.width,
                    borderBottom: "1px solid var(--color-border-subtle)",
                  }}
                >
                  <span style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
                    {c.label}
                    {sort === c.key &&
                      (order === "asc" ? <ArrowUp size={11} /> : <ArrowDown size={11} />)}
                  </span>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {isLoading && items.length === 0 && (
              <tr>
                <td
                  colSpan={COLUMNS.length}
                  style={{ padding: "40px 14px", textAlign: "center", color: "var(--color-text-muted)" }}
                >
                  Loading…
                </td>
              </tr>
            )}
            {error && (
              <tr>
                <td
                  colSpan={COLUMNS.length}
                  style={{ padding: "40px 14px", textAlign: "center", color: "var(--color-danger)" }}
                >
                  Failed to load activity.
                </td>
              </tr>
            )}
            {!isLoading && !error && items.length === 0 && (
              <tr>
                <td
                  colSpan={COLUMNS.length}
                  style={{ padding: "60px 14px", textAlign: "center" }}
                >
                  <Activity size={28} style={{ color: "var(--color-text-muted)", marginBottom: 10 }} />
                  <div style={{ fontSize: 14, fontWeight: 500, color: "var(--color-text-secondary)" }}>
                    {search ? "No matches" : "No activity yet"}
                  </div>
                  <div style={{ fontSize: 12, color: "var(--color-text-muted)", marginTop: 4 }}>
                    {search
                      ? "Try a different search term."
                      : "Once you add a torrent, it'll appear here."}
                  </div>
                </td>
              </tr>
            )}
            {items.map((it) => (
              <Row key={it.info_hash} item={it} />
            ))}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {total > PAGE_SIZE && (
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            marginTop: 12,
            fontSize: 12,
            color: "var(--color-text-muted)",
          }}
        >
          <div>
            Showing {offset + 1}–{Math.min(offset + items.length, total)} of {total.toLocaleString()}
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <PageButton
              disabled={offset === 0}
              onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
              icon={<ChevronLeft size={14} />}
              label="Prev"
            />
            <span>
              Page {page} of {totalPages}
            </span>
            <PageButton
              disabled={offset + PAGE_SIZE >= total}
              onClick={() => setOffset(offset + PAGE_SIZE)}
              icon={<ChevronRight size={14} />}
              label="Next"
              iconRight
            />
          </div>
        </div>
      )}
    </div>
  );
}

function Row({ item }: { item: ActivityItem }) {
  const status = useMemo(() => statusOf(item), [item]);

  return (
    <tr
      style={{
        borderBottom: "1px solid var(--color-border-subtle)",
      }}
    >
      <td style={{ padding: "10px 14px", maxWidth: 0 }}>
        <Link
          to={`/activity/${item.info_hash}`}
          style={{
            color: "var(--color-text-primary)",
            textDecoration: "none",
            fontWeight: 500,
            display: "block",
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
          title={item.name}
        >
          {item.name || item.info_hash.slice(0, 12)}
        </Link>
        {item.category && (
          <div
            style={{
              fontSize: 11,
              color: "var(--color-text-muted)",
              marginTop: 2,
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
            }}
          >
            {item.category}
          </div>
        )}
      </td>
      <td
        style={{
          padding: "10px 14px",
          textAlign: "right",
          color: "var(--color-text-secondary)",
          fontVariantNumeric: "tabular-nums",
        }}
      >
        {item.size_bytes > 0 ? formatBytes(item.size_bytes) : "—"}
      </td>
      <td style={{ padding: "10px 14px" }}>
        {item.resolution ? <ResolutionBadge value={item.resolution} /> : <span style={{ color: "var(--color-text-muted)" }}>—</span>}
      </td>
      <td style={{ padding: "10px 14px", color: "var(--color-text-secondary)" }}>
        {timeAgo(item.added_at)}
      </td>
      <td style={{ padding: "10px 14px" }}>
        <StatusPill status={status} />
      </td>
    </tr>
  );
}

type RowStatus = "removed" | "completed" | "downloading";

function statusOf(it: ActivityItem): RowStatus {
  if (it.removed_at) return "removed";
  if (it.completed_at) return "completed";
  return "downloading";
}

function StatusPill({ status }: { status: RowStatus }) {
  const map: Record<RowStatus, { color: string; bg: string; label: string; Icon: React.ElementType }> = {
    removed: { color: "var(--color-text-muted)", bg: "var(--color-bg-elevated)", label: "Removed", Icon: Trash2 },
    completed: { color: "var(--color-success)", bg: "color-mix(in srgb, var(--color-success) 12%, transparent)", label: "Completed", Icon: CheckCircle },
    downloading: { color: "var(--color-accent)", bg: "color-mix(in srgb, var(--color-accent) 12%, transparent)", label: "Downloading", Icon: Download },
  };
  const { color, bg, label, Icon } = map[status];
  return (
    <span
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 4,
        padding: "3px 8px",
        borderRadius: 4,
        background: bg,
        color,
        fontSize: 11,
        fontWeight: 500,
      }}
    >
      <Icon size={11} /> {label}
    </span>
  );
}

function ResolutionBadge({ value }: { value: string }) {
  const colorMap: Record<string, string> = {
    "2160p": "var(--color-accent)",
    "1080p": "var(--color-success)",
    "720p": "var(--color-text-secondary)",
    "480p": "var(--color-text-muted)",
  };
  const color = colorMap[value] ?? "var(--color-text-muted)";
  return (
    <span
      style={{
        display: "inline-block",
        padding: "2px 6px",
        borderRadius: 4,
        border: `1px solid ${color}`,
        color,
        fontSize: 10,
        fontWeight: 600,
        letterSpacing: 0.4,
      }}
    >
      {value.toUpperCase()}
    </span>
  );
}

function PageButton({
  disabled,
  onClick,
  icon,
  label,
  iconRight,
}: {
  disabled: boolean;
  onClick: () => void;
  icon: React.ReactNode;
  label: string;
  iconRight?: boolean;
}) {
  return (
    <button
      disabled={disabled}
      onClick={onClick}
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 4,
        padding: "5px 10px",
        fontSize: 12,
        borderRadius: 6,
        border: "1px solid var(--color-border-default)",
        background: "var(--color-bg-surface)",
        color: disabled ? "var(--color-text-muted)" : "var(--color-text-secondary)",
        cursor: disabled ? "not-allowed" : "pointer",
        opacity: disabled ? 0.5 : 1,
      }}
    >
      {!iconRight && icon}
      {label}
      {iconRight && icon}
    </button>
  );
}
