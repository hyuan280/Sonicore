import { cn } from "../../lib/utils"

interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "primary" | "ghost" | "danger"
  size?: "sm" | "md" | "lg"
}

export function Button({ className, variant = "primary", size = "md", ...props }: ButtonProps) {
  return (
    <button
      className={cn(
        "inline-flex items-center justify-center rounded-lg font-medium transition-colors disabled:opacity-50 cursor-pointer",
        variant === "primary" && "bg-green-600 hover:bg-green-500 text-white",
        variant === "ghost" && "hover:bg-zinc-800 text-zinc-300 hover:text-white",
        variant === "danger" && "bg-red-600 hover:bg-red-500 text-white",
        size === "sm" && "px-3 py-1.5 text-sm",
        size === "md" && "px-4 py-2 text-sm",
        size === "lg" && "px-6 py-3 text-base",
        className,
      )}
      {...props}
    />
  )
}
