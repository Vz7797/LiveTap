try {
  if (localStorage.getItem("livetap-theme") === "light") document.documentElement.classList.remove("dark");
} catch (_) {}
