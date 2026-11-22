// Palettes, shared with danieldaum.net. Saturn's rule: one palette per visit,
// and the next visit gets the next one. On top of that, a `theme` cookie for
// the parent domain carries the palette between danieldaum.net and this
// site, both ways. This runs in <head>, before the first paint. Without JS
// the page keeps the palette from config and the footer buttons stay hidden.
const themes = ["moss", "ink", "wine", "plum", "teal"];
const root = document.documentElement;

// code.danieldaum.net shares cookies with danieldaum.net; an IP or
// localhost gets a host-only cookie.
const host = location.hostname;
const domain = /^[\d.]+$|^[^.]+$/.test(host) ? "" : `; domain=${host.split(".").slice(-2).join(".")}`;

function after(theme) {
    return themes[(themes.indexOf(theme) + 1) % themes.length];
}

function use(theme) {
    root.dataset.theme = theme;
    document.cookie = `theme=${theme}; path=/; samesite=lax${domain}${location.protocol === "https:" ? "; secure" : ""}`;
    try {
        sessionStorage.setItem("theme", theme);
        localStorage.setItem("theme", theme);
    } catch {}
}

let current = "moss";
try {
    const shared = /(?:^|;\s*)theme=([a-z]+)/.exec(document.cookie)?.[1];
    const visit = sessionStorage.getItem("theme");
    current = themes.includes(shared) ? shared : themes.includes(visit) ? visit : after(localStorage.getItem("theme"));
} catch {}
use(current);

// Font review (temporary): Geist Pixel for all text, remembered per browser.
function font(name) {
    if (name === "pixel") root.dataset.font = "pixel";
    else delete root.dataset.font;
    try {
        localStorage.setItem("font", name);
    } catch {}
}
try {
    font(localStorage.getItem("font") || "geist");
} catch {}

document.addEventListener("DOMContentLoaded", () => {
    for (const button of document.querySelectorAll(".theme-button, .font-button")) button.hidden = false;
});
document.addEventListener("click", (event) => {
    if (event.target.closest(".theme-button")) use((current = after(current)));
    if (event.target.closest(".font-button")) font(root.dataset.font === "pixel" ? "geist" : "pixel");
});
