import { useEffect, useRef, type ButtonHTMLAttributes, type ReactNode } from "react";
import type { StockLevel } from "../domain/types";
import { cx } from "../lib/format";

type ButtonVariant = "default" | "primary" | "dark";

const VARIANTS: Record<ButtonVariant, string> = {
  default: "border-line bg-surface text-ink",
  primary: "border-safety bg-safety text-asphalt font-head uppercase tracking-wide leading-tight",
  dark: "border-ink bg-ink text-bg",
};

export function Button({
  variant = "default",
  className,
  type = "button",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant }) {
  return (
    <button
      type={type}
      className={cx(
        "inline-flex min-h-12 cursor-pointer items-center justify-center gap-2 rounded-xl border px-4 py-2 font-semibold disabled:cursor-not-allowed disabled:opacity-50",
        VARIANTS[variant],
        className,
      )}
      {...props}
    />
  );
}

export function Card({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cx("rounded-xl border border-line bg-surface p-3.5 shadow-sm", className)}>{children}</div>;
}

const PILL_STYLES = {
  crit: "bg-crit-soft text-crit",
  low: "bg-low-soft text-low",
  ok: "bg-ok-soft text-ok",
  info: "bg-info-soft text-info",
} as const;

export type PillTone = keyof typeof PILL_STYLES;

export function Pill({ tone, children }: { tone: PillTone; children: ReactNode }) {
  return (
    <span className={cx("inline-flex items-center gap-1.5 whitespace-nowrap rounded-full px-2.5 py-0.5 text-sm font-semibold", PILL_STYLES[tone])}>
      <span aria-hidden className="size-2 rounded-full bg-current" />
      {children}
    </span>
  );
}

const LEVEL_PILL: Record<StockLevel, { tone: PillTone; label: string }> = {
  out: { tone: "crit", label: "Tükendi" },
  critical: { tone: "crit", label: "Kritik" },
  low: { tone: "low", label: "Düşük" },
  ok: { tone: "ok", label: "Yeterli" },
};

/** Stok durumu hem renkle hem yazıyla gösterilir */
export function StockPill({ level }: { level: StockLevel }) {
  const { tone, label } = LEVEL_PILL[level];
  return <Pill tone={tone}>{label}</Pill>;
}

export const stockLabel = (level: StockLevel): string => LEVEL_PILL[level].label;

export function Segmented<T extends string>({
  label,
  options,
  value,
  onChange,
}: {
  label: string;
  options: { value: T; label: string }[];
  value: T;
  onChange: (value: T) => void;
}) {
  return (
    <div role="group" aria-label={label} className="inline-flex overflow-hidden rounded-xl border border-line bg-surface">
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          aria-pressed={option.value === value}
          onClick={() => onChange(option.value)}
          className={cx(
            "min-h-12 cursor-pointer px-4 font-semibold",
            option.value === value ? "bg-ink text-bg" : "text-ink",
          )}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

export function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className="grid gap-1.5 text-sm font-semibold">
      <span>
        {label}
        {hint && <span className="ml-1.5 font-normal text-muted">{hint}</span>}
      </span>
      {children}
    </label>
  );
}

export function ErrorText({ children }: { children: ReactNode }) {
  return (
    <p role="alert" className="min-h-5 text-sm font-semibold text-crit">
      {children}
    </p>
  );
}

export function EmptyState({ children }: { children: ReactNode }) {
  return <div className="rounded-xl border border-dashed border-line px-3 py-7 text-center text-muted">{children}</div>;
}

/** Yerel <dialog> üzerine kurulu modal */
export function Modal({
  open,
  title,
  onClose,
  children,
}: {
  open: boolean;
  title: string;
  onClose: () => void;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const dialog = ref.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    if (!open && dialog.open) dialog.close();
  }, [open]);

  return (
    <dialog
      ref={ref}
      onClose={onClose}
      className="m-auto w-[min(560px,calc(100%-24px))] rounded-2xl bg-surface p-0 text-ink"
    >
      <div className="flex items-center justify-between border-b border-line px-4 py-3">
        <h3 className="text-lg">{title}</h3>
        <button
          type="button"
          onClick={onClose}
          aria-label="Kapat"
          className="size-11 cursor-pointer rounded-lg border border-line text-xl"
        >
          ×
        </button>
      </div>
      <div className="grid gap-3 p-4">{open && children}</div>
    </dialog>
  );
}
