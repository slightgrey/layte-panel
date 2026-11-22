// Applies the saved theme before React renders, so there is no flash of the
// wrong colours. Kept as a file (not inline) because the CSP blocks inline scripts.
(function () {
  try {
    var t = localStorage.getItem("layte-theme") || "system";
    var dark = t === "dark" || (t === "system" && matchMedia("(prefers-color-scheme: dark)").matches);
    document.documentElement.classList.toggle("dark", dark);
  } catch (e) {}
})();
