import type { ComponentProps } from "react";

/* The ordinary button: the size of the bulk bar's and the side panels'
 * actions. Outlined is the default; filled is the one thing the page
 * most wants done; danger is a write that cannot be taken back, in the
 * alert colour and never the default.
 *
 * The compact buttons inside table rows are a different size and live
 * in closeout/Table.tsx as `small`; the raised buttons in a tool's
 * header are HeaderButton in ToolHeader.tsx. */

const VARIANT = {
  outlined: { background: "var(--surface)", border: "1px solid var(--line)", color: "var(--ink)" },
  filled: { background: "var(--accent)", color: "#fff" },
  danger: { background: "var(--surface)", border: "1px solid var(--crit)", color: "var(--crit)" },
} as const;

export function Button({
  variant = "outlined", className = "", children, ...rest
}: ComponentProps<"button"> & { variant?: keyof typeof VARIANT }) {
  return (
    <button
      {...rest}
      className={`rounded-lg px-3 py-1.5 text-[13px] font-medium disabled:opacity-40 ${className}`.trim()}
      style={VARIANT[variant]}
    >
      {children}
    </button>
  );
}
