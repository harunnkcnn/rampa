// Small progressive enhancements. Everything works without this file.
document.addEventListener("click", (e) => {
  const btn = e.target.closest("[data-print]");
  if (btn) {
    e.preventDefault();
    window.print();
  }
});

// Keep keyboard focus useful after htmx swaps a form region.
document.addEventListener("htmx:afterSwap", (e) => {
  const target = e.detail.target;
  if (!target) return;
  const focus = target.querySelector("[data-autofocus]");
  if (focus) focus.focus();
});

// Submit selects marked data-autosubmit (warehouse switcher).
document.addEventListener("change", (e) => {
  const el = e.target;
  if (el.matches && el.matches("select[data-autosubmit]") && el.form) el.form.submit();
});
