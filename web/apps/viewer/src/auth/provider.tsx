/**
 * Who is signed in.
 *
 * The session is held in memory and nowhere else. Authsome issues a bearer
 * token rather than setting an HTTP-only cookie, so the token has to live
 * somewhere the page can reach — and the only choice left is how widely. In
 * localStorage or sessionStorage it is readable by anything that manages to
 * run in the page, and it survives the tab; in a module variable it is
 * readable by the same things and dies with the tab.
 *
 * So a reload signs the operator out. That is a real cost and it is the
 * conservative side of a trade this console should not make quietly: an
 * advisory grid surface is exactly the kind of thing where a stolen session is
 * worth more than the inconvenience of signing in again. If it becomes
 * intolerable, the fix is the gateway setting an HTTP-only cookie, not a
 * more convenient place to keep a token.
 */

import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";

import { signIn, signUp, type Credentials, type Session } from "./authsome";

interface AuthState {
  readonly session: Session | null;
  readonly publishableKey: string;
  readonly signIn: (credentials: Credentials) => Promise<void>;
  readonly signUp: (credentials: Credentials) => Promise<void>;
  readonly signOut: () => void;
}

const AuthContext = createContext<AuthState | null>(null);

export interface AuthProviderProps {
  /**
   * Identifies which Authsome app a credential belongs to. Publishable by
   * design — it grants nothing on its own.
   */
  readonly publishableKey: string;
  readonly baseUrl?: string;
  readonly children: ReactNode;
}

export function AuthProvider({
  publishableKey,
  baseUrl = "",
  children,
}: AuthProviderProps): React.JSX.Element {
  const [session, setSession] = useState<Session | null>(null);

  const doSignIn = useCallback(
    async (credentials: Credentials) => {
      setSession(await signIn(credentials, publishableKey, baseUrl));
    },
    [publishableKey, baseUrl],
  );

  const doSignUp = useCallback(
    async (credentials: Credentials) => {
      setSession(await signUp(credentials, publishableKey, baseUrl));
    },
    [publishableKey, baseUrl],
  );

  const value = useMemo<AuthState>(
    () => ({
      session,
      publishableKey,
      signIn: doSignIn,
      signUp: doSignUp,
      signOut: () => setSession(null),
    }),
    [session, publishableKey, doSignIn, doSignUp],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const state = useContext(AuthContext);

  if (state === null) {
    throw new Error("useAuth: no AuthProvider above this component");
  }

  return state;
}
