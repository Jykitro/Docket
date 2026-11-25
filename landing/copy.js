const button = document.getElementById("copy-commands");
const commands = document.getElementById("commands");
const feedback = document.getElementById("copy-status");

if (navigator.clipboard?.writeText) {
  button.hidden = false;
  button.addEventListener("click", async () => {
    let message;
    try {
      await navigator.clipboard.writeText(commands.textContent);
      message = "Commands copied.";
    } catch {
      message = "Select the commands and copy them manually.";
    }
    feedback.className = "copy-feedback";
    feedback.textContent = message;
  });
}

// Carry campaign attribution to the planner without forwarding unrelated query parameters.
const campaign = new URLSearchParams(location.search);
for (const link of document.querySelectorAll("a[data-app]")) {
  const target = new URL(link.href);
  for (const [name, value] of campaign) {
    if (/^utm_[a-z0-9_]+$/i.test(name)) target.searchParams.append(name, value);
  }
  link.href = target.href;
}
