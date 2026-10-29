const dateTime = new Intl.DateTimeFormat("tr-TR", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" });
const longDate = new Intl.DateTimeFormat("tr-TR", { weekday: "long", day: "numeric", month: "long" });

export const formatDateTime = (ts: number): string => dateTime.format(ts);
export const formatToday = (): string => longDate.format(Date.now());

export const cx = (...classes: (string | false | null | undefined)[]): string => classes.filter(Boolean).join(" ");
