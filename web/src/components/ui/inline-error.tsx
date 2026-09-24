// InlineError is a small error bubble centered under the nearest positioned
// ancestor. The owner is responsible for dismissing it (e.g. via an
// outside-click handler).
export function InlineError({ message }: { message: string }) {
  return (
    <span
      role="alert"
      className="absolute top-full left-1/2 -translate-x-1/2 mt-1 z-[70] whitespace-nowrap rounded-md bg-red-600 px-2 py-1 text-xs text-white shadow-lg"
    >
      {message}
    </span>
  );
}
