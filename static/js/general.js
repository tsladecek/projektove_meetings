function displayError(msg) {
  Toastify({
    text: msg,
    duration: 10000,
    newWindow: true,
    close: true,
    gravity: "top",
    position: "right",
    stopOnFocus: true,
    style: {
      background: "#CC0000",
    },
    onClick: function () {},
  }).showToast();
}

function displaySuccess(msg) {
  Toastify({
    text: msg,
    duration: 2000,
    newWindow: true,
    close: true,
    gravity: "top",
    position: "right",
    stopOnFocus: true,
    style: {
      background: "#007700",
    },
    onClick: function () {},
  }).showToast();
}

// toasts
document.addEventListener("htmx:responseError", (e) => {
  displayError(e.detail.xhr.responseText.trim());
});

document.addEventListener("htmx:afterRequest", (e) => {
  const t = e.detail.xhr.getResponseHeader("X-Toast");
  if (t) {
    displaySuccess(t);
  }
});

document.addEventListener("htmx:afterProcessNode", function (event) {
  const tooltips = event.target.querySelectorAll(".nodeWithTooltip");

  if (
    event.target.classList &&
    event.target.classList.contains("nodeWithTooltip")
  ) {
    initializeTippy(event.target);
  }

  tooltips.forEach((t) => initializeTippy(t));
});

function initializeTippy(t) {
  const tc = t.querySelector(".nodeTooltipContent");
  const tagName = t.tagName.toLowerCase();
  if (
    tc &&
    (tagName !== "td" ||
      t.scrollWidth > t.clientWidth ||
      t.scrollHeight > t.clientHeight)
  ) {
    tippy(t, { content: tc.innerHTML, allowHTML: true });
  }
}

document.addEventListener("DOMContentLoaded", function () {
  document
    .querySelectorAll(".nodeWithTooltip")
    .forEach((t) => initializeTippy(t));
});
