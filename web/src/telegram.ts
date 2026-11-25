import { useEffect } from "react";

type TelegramButton = {
  show(): void;
  hide(): void;
  onClick(handler: () => void): void;
  offClick(handler: () => void): void;
};

type TelegramWebApp = {
  initData: string;
  colorScheme: "light" | "dark";
  ready(): void;
  expand(): void;
  onEvent(event: "themeChanged", handler: () => void): void;
  offEvent(event: "themeChanged", handler: () => void): void;
  BackButton?: TelegramButton;
  MainButton?: TelegramButton & {
    setText(text: string): void;
    enable(): void;
    disable(): void;
  };
};

const app = (window as Window & { Telegram?: { WebApp?: TelegramWebApp } })
  .Telegram?.WebApp;
export const telegram = app?.initData ? app : undefined;

export function useTelegramMainButton(
  form: React.RefObject<HTMLFormElement | null>,
  visible: boolean,
  pending: boolean,
  label: string,
) {
  useEffect(() => {
    const button = telegram?.MainButton;
    if (!button || !visible) return;
    const submit = () => form.current?.requestSubmit();
    button.setText(label);
    if (pending) button.disable();
    else button.enable();
    button.onClick(submit);
    button.show();
    return () => {
      button.offClick(submit);
      button.hide();
    };
  }, [form, label, pending, visible]);
}
