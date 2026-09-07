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
document.addEventListener("htmx:response:error", (e) => {
  displayError(e.detail.xhr.responseText.trim());
});

document.addEventListener("htmx:after:request", (e) => {
  const t = e.detail.ctx.response.headers.get("x-toast");
  if (t) {
    displaySuccess(t);
  }
  const err = e.detail.ctx.response.headers.get("x-error");
  if (err) {
    displayError(err);
  }
});

function submitAll() {
  let firstInvalid = null;
  document.querySelectorAll("form[id^='issue-']").forEach((form) => {
    if (!form.checkValidity() && !firstInvalid) {
      firstInvalid = form;
    }
  });
  if (firstInvalid) {
    firstInvalid.reportValidity();
    return;
  }
  document.querySelectorAll("[data-submit-issue]").forEach((btn) => {
    btn.disabled = true;
    htmx.trigger(btn, "click");
  });
}

document.addEventListener("htmx:afterSwap", (e) => {
  const row = document.getElementById("submit-all-row");
  if (row && !document.querySelector("[data-submit-issue]")) {
    row.classList.add("hidden");
  }
});

document.addEventListener("htmx:after:process:node", function (event) {
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
