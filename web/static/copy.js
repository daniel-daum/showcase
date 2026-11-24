// Copy buttons, as progressive enhancement: without JS the buttons stay
// hidden and the text can still be copied by hand. data-copy names a
// read-only field, whose value is copied, or a link to a raw file, which is
// fetched and copied as it is (the CSP allows fetches to this origin only).
// Nothing inline: the CSP forbids it.
function copyFile(url) {
    const file = fetch(url).then((r) => (r.ok ? r.blob() : Promise.reject(new Error(r.statusText))));
    // Safari only allows clipboard writes during the click, so the
    // download goes in as a promise; elsewhere writeText after it is fine.
    if (window.ClipboardItem) {
        const blob = file.then((b) => new Blob([b], { type: "text/plain" }));
        return navigator.clipboard.write([new ClipboardItem({ "text/plain": blob })]);
    }
    return file.then((b) => b.text()).then((t) => navigator.clipboard.writeText(t));
}

for (const button of document.querySelectorAll("button[data-copy]")) {
    const field = document.getElementById(button.dataset.copy);
    if (!field || !navigator.clipboard) continue;
    button.hidden = false;
    button.addEventListener("click", async () => {
        try {
            await (field.href ? copyFile(field.href) : navigator.clipboard.writeText(field.value));
            button.classList.add("copied");
            setTimeout(() => button.classList.remove("copied"), 1500);
        } catch {
            field.select?.();
        }
    });
}
