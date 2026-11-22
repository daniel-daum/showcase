// Copy buttons, as progressive enhancement: without JS the buttons stay
// hidden and the read-only fields can be selected and copied by hand. This is
// the only script on the site; the CSP allows nothing inline.
for (const button of document.querySelectorAll("button[data-copy]")) {
    const field = document.getElementById(button.dataset.copy);
    if (!field || !navigator.clipboard) continue;
    button.hidden = false;
    button.addEventListener("click", async () => {
        try {
            await navigator.clipboard.writeText(field.value);
            button.classList.add("copied");
            setTimeout(() => button.classList.remove("copied"), 1500);
        } catch {
            field.select();
        }
    });
}
