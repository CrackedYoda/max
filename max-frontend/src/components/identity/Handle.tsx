import { cx } from "../ui/cx";

/** `max.yoda` with the clay prefix, as everywhere on the canvas. */
export function Handle({ name, className }: { name: string; className?: string }) {
  const bare = name.replace(/^max\./, "");
  return (
    <span className={cx("font-mono font-semibold", className)}>
      <span className="text-accent">max.</span>
      {bare}
    </span>
  );
}
