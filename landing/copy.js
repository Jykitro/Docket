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
