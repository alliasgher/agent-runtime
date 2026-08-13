"use client";

import { DocumentInfo } from "@/lib/types";

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function FileIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth={1.8}
        d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z"
      />
    </svg>
  );
}

/**
 * The strip of attached documents above the message box. Hidden entirely when
 * nothing is attached, so the input area is unchanged for users who never
 * upload anything.
 */
export default function DocumentBar({
  documents,
  uploading,
  onRemove,
}: {
  documents: DocumentInfo[];
  uploading: string[];
  onRemove: (doc: DocumentInfo) => void;
}) {
  if (documents.length === 0 && uploading.length === 0) return null;

  return (
    <div className="mb-2.5 flex flex-wrap gap-2">
      {documents.map((doc) => (
        <div
          key={doc.id}
          className="group flex max-w-[240px] items-center gap-2 rounded-lg border border-prism-border bg-prism-surface py-1.5 pl-2.5 pr-1.5"
        >
          <FileIcon className="h-4 w-4 flex-shrink-0 text-prism-navy" />
          <div className="min-w-0 flex-1">
            <p className="truncate text-xs font-medium text-prism-deep" title={doc.name}>
              {doc.name}
            </p>
            <p className="text-[10px] text-prism-muted">
              {doc.num_chunks} passage{doc.num_chunks === 1 ? "" : "s"} · {formatSize(doc.size_bytes)}
            </p>
          </div>
          <button
            onClick={() => onRemove(doc)}
            title={`Remove ${doc.name}`}
            aria-label={`Remove ${doc.name}`}
            className="flex-shrink-0 rounded p-1 text-prism-muted transition-colors hover:bg-prism-border hover:text-prism-coral"
          >
            <svg className="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>
      ))}

      {uploading.map((name) => (
        <div
          key={`uploading-${name}`}
          className="flex max-w-[240px] items-center gap-2 rounded-lg border border-prism-border bg-prism-surface px-2.5 py-1.5"
        >
          <svg className="h-4 w-4 flex-shrink-0 animate-spin text-prism-navy" viewBox="0 0 24 24" fill="none">
            <circle className="opacity-20" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" />
            <path d="M12 2a10 10 0 0 1 10 10" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
          </svg>
          <div className="min-w-0">
            <p className="truncate text-xs font-medium text-prism-deep" title={name}>
              {name}
            </p>
            <p className="text-[10px] text-prism-muted">Indexing…</p>
          </div>
        </div>
      ))}
    </div>
  );
}
