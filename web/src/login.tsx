import { validateForm } from "./form";
import { useState, type FormEvent } from "react";
import { Eye, EyeOff, LockKeyhole, Sun, Activity } from "lucide-react";
import { Brand } from "./ui";
import { login } from "./core";
import field from "./assets/solar-field.jpg";
export function Login() {
  const [show, setShow] = useState(false),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (busy) return;
    const f = new FormData(e.currentTarget);
    setBusy(true);
    setError("");
    try {
      await login(String(f.get("username")).trim(), String(f.get("password")));
    } catch (e) {
      setError(
        e instanceof TypeError
          ? "Нет связи с сервером. Проверьте подключение."
          : (e as Error).message,
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="entry">
      <section
        className="entry-photo"
        style={{ backgroundImage: "url(" + field + ")" }}
      >
        <Brand />
        <div className="entry-caption">
          <span className="photo-label">
            <Sun size={16} /> Солнечная энергетика
          </span>
          <h1>
            Управление <br />
            солнечными <br />
            <em>станциями.</em>
          </h1>
          <p>Оборудование, мониторинг и обслуживание в одной системе.</p>
        </div>
      </section>
      <section className="entry-form">
        <div className="entry-inner">
          <span className="entry-symbol">
            <Activity size={26} />
          </span>
          <h2>Вход в систему</h2>
          <p className="muted">Введите данные вашей учётной записи.</p>
          <form noValidate onSubmitCapture={validateForm} onSubmit={submit}>
            <label>
              Логин
              <input
                name="username"
                autoComplete="username"
                required
                placeholder="Ваш логин"
                autoCapitalize="none"
              />
            </label>
            <label>
              Пароль
              <span className="password">
                <input
                  name="password"
                  type={show ? "text" : "password"}
                  autoComplete="current-password"
                  required
                  placeholder="Введите пароль"
                />
                <button
                  type="button"
                  className="iconbtn"
                  aria-label={show ? "Скрыть пароль" : "Показать пароль"}
                  onClick={() => setShow(!show)}
                >
                  {show ? <EyeOff size={18} /> : <Eye size={18} />}
                </button>
              </span>
            </label>
            {error && (
              <p className="failure" role="alert">
                {error}
              </p>
            )}
            <button className="btn primary" disabled={busy}>
              {busy ? "Входим…" : "Войти"}
            </button>
          </form>
          <p className="entry-note">
            <LockKeyhole size={15} /> Для получения доступа обратитесь к
            администратору.
          </p>
        </div>
      </section>
    </main>
  );
}
