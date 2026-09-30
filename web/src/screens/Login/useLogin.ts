// Logic for the login screen.

import { useState } from "react";
import { login } from "../../api/client";

export interface LoginLogic {
  password: string;
  setPassword: (value: string) => void;
  error: string | null;
  submitting: boolean;
  submit: () => void;
}

/** onLoggedIn is called after a successful login. */
export function useLogin(onLoggedIn: () => void): LoginLogic {
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function submit() {
    setSubmitting(true);
    setError(null);
    try {
      await login(password);
      setPassword("");
      onLoggedIn();
    } catch (err) {
      // The device's message explains wrong passwords and rate limiting.
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSubmitting(false);
    }
  }

  return { password, setPassword, error, submitting, submit: () => void submit() };
}
