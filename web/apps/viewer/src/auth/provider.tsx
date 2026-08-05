/**
 * Who is signed in.
 *
 * The session lives in an HttpOnly cookie the platform sets, so this holds an
 * identity and never a credential — and a reload keeps the operator signed in,
 * because the cookie outlives the page while nothing here does.
 *
 * Three states rather than two. `checking` is the interval before `/me`
 * answers, and folding it into `anonymous` flashes the login form at someone
 * who is already signed in on every reload. That is invisible in development
 * against a warm localhost, which is exactly why it is a state rather than a
 * detail.
 */

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

import { me, signIn, signOut, signUp, type Credentials, type Session } from "./authsome";

/** Where the answer to "is anyone signed in" has got to. */
export type AuthStatus = "checking" | "anonymous" | "authenticated";

interface AuthState {
  readonly status: AuthStatus;
  /** Non-null exactly when `status` is `authenticated`. */
  readonly session: Session | null;
  readonly publishableKey: string;
  readonly signIn: (credentials: Credentials) => Promise<void>;
  readonly signUp: (credentials: Credentials) => Promise<void>;
  readonly signOut: () => Promise<void>;
  /** The session could not be refreshed. Called by the API client, not by a screen. */
  readonly expire: () => void;
}

const AuthContext = createContext<AuthState | null>(null);

export interface AuthProviderProps {
  /**
   * Identifies which Authsome app a credential belongs to. Publishable by
   * design — it grants nothing on its own.
   */
  readonly publishableKey: string;
  readonly children: ReactNode;
}

export function AuthProvider({ publishableKey, children }: AuthProviderProps): React.JSX.Element {
  const [status, setStatus] = useState<AuthStatus>("checking");
  const [session, setSession] = useState<Session | null>(null);

  const adopt = useCallback((next: Session) => {
    setSession(next);
    setStatus("authenticated");
  }, []);

  const forget = useCallback(() => {
    setSession(null);
    setStatus("anonymous");
  }, []);

  useEffect(() => {
    let cancelled = false;

    me()
      .then((found) => {
        if (cancelled) return;
        if (found === null) forget();
        else adopt(found);
      })
      .catch(() => {
        // The host is unreachable or broken. Stranding the console on a splash
        // screen would leave the reader nothing to act on; the login form at
        // least fails in a way they can retry.
        if (!cancelled) forget();
      });

    return () => {
      cancelled = true;
    };
  }, [adopt, forget]);

  const doSignIn = useCallback(
    async (credentials: Credentials) => {
      adopt(await signIn(credentials, publishableKey));
    },
    [adopt, publishableKey],
  );

  const doSignUp = useCallback(
    async (credentials: Credentials) => {
      adopt(await signUp(credentials, publishableKey));
    },
    [adopt, publishableKey],
  );

  const doSignOut = useCallback(async () => {
    // Local state is cleared whatever the host says. The operator asked to be
    // signed out, and a request that failed is not a reason to leave them
    // looking at a console they have finished with.
    try {
      await signOut();
    } finally {
      forget();
    }
  }, [forget]);

  const value = useMemo<AuthState>(
    () => ({
      status,
      session,
      publishableKey,
      signIn: doSignIn,
      signUp: doSignUp,
      signOut: doSignOut,
      expire: forget,
    }),
    [status, session, publishableKey, doSignIn, doSignUp, doSignOut, forget],
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
