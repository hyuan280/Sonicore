import { useEffect, useRef, useState } from "react";

// useInlineError manages a nullable error value that is cleared when the
// pointer is pressed outside the returned anchor element. Callers render an
// <InlineError> bubble inside the anchored element, so all inline action errors
// share the same dismissal behaviour.
export function useInlineError<T = string, E extends HTMLElement = HTMLDivElement>() {
  const [error, setError] = useState<T | null>(null);
  const anchorRef = useRef<E>(null);

  useEffect(() => {
    if (error == null) return;
    const handler = (e: MouseEvent) => {
      if (anchorRef.current && !anchorRef.current.contains(e.target as Node)) {
        setError(null);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [error]);

  return { error, setError, anchorRef };
}
