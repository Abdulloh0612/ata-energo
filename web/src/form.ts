import type { FormEvent } from "react";

let issueSequence = 0;

type Field = HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement;

function clearIssue(field: Field) {
  const id = field.dataset.validationIssue;
  if (!id) return;
  document.getElementById(id)?.remove();
  const descriptions = field
    .getAttribute("aria-describedby")
    ?.split(" ")
    .filter((value) => value !== id);
  if (descriptions?.length)
    field.setAttribute("aria-describedby", descriptions.join(" "));
  else field.removeAttribute("aria-describedby");
  field.removeAttribute("aria-invalid");
  delete field.dataset.validationIssue;
}

export function validateForm(event: FormEvent<HTMLFormElement>) {
  const fields = Array.from(event.currentTarget.elements).filter(
    (element): element is Field =>
      element instanceof HTMLInputElement ||
      element instanceof HTMLSelectElement ||
      element instanceof HTMLTextAreaElement,
  );
  fields.forEach(clearIssue);
  const field = fields.find(
    (element) => element.willValidate && !element.validity.valid,
  );
  if (!field) return;
  event.preventDefault();
  event.stopPropagation();
  const msg = document.createElement("span");
  issueSequence += 1;
  msg.id = "ata-field-" + issueSequence;
  msg.className = "field-error";
  msg.setAttribute("role", "alert");
  const v = field.validity;
  msg.textContent = v.valueMissing
    ? "Заполните это поле."
    : v.typeMismatch
      ? "Проверьте формат значения."
      : v.tooShort
        ? "Введите не менее " +
          (field as HTMLInputElement).minLength +
          " символов."
        : v.patternMismatch
          ? "Проверьте формат поля. " +
            (field.title || field.getAttribute("placeholder") || "")
          : "Проверьте введённое значение.";
  field.closest("label")?.after(msg);
  field.dataset.validationIssue = msg.id;
  field.setAttribute("aria-invalid", "true");
  field.setAttribute(
    "aria-describedby",
    [field.getAttribute("aria-describedby"), msg.id].filter(Boolean).join(" "),
  );
  field.addEventListener("input", () => clearIssue(field), { once: true });
  field.focus();
}
