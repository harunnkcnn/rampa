import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from "react";
import { cx } from "../lib/format";

const ToastContext = createContext<(message: string) => void>(() => {});

export const useToast = () => useContext(ToastContext);

export function ToastProvider({ children }: { children: ReactNode }) {
  const [message, setMessage] = useState("");
  const [visible, setVisible] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const show = useCallback((text: string) => {
    setMessage(text);
    setVisible(true);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setVisible(false), 2400);
  }, []);

  return (
    <ToastContext.Provider value={show}>
      {children}
      <div
        role="status"
        aria-live="polite"
        className={cx(
          "pointer-events-none fixed left-1/2 z-20 max-w-[calc(100%-32px)] -translate-x-1/2 rounded-lg bg-ink px-4 py-2.5 font-semibold text-bg transition-opacity",
          visible ? "opacity-100" : "opacity-0",
        )}
        style={{ bottom: "calc(88px + env(safe-area-inset-bottom))" }}
      >
        {message}
      </div>
    </ToastContext.Provider>
  );
}
