import { useContext, useEffect, useState } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { LoginContext } from "./LoginContext";
import axios from "./axios";

type ProtectedRouteProps = { children: React.ReactNode; permission?: string };

// The UI checks current server permissions; APIs independently enforce the same rules.
export const ProtectedRoute = ({children, permission}: ProtectedRouteProps) => {
  const location = useLocation();
  const context = useContext(LoginContext);
  if (context === undefined) throw new Error("LoginContext provider required");
  const { user } = context;
  const [access, setAccess] = useState<boolean | null>(null);
  useEffect(() => {
    let active = true;
    setAccess(null);
    if (user && permission) {
      axios.get("/api/rbac/me").then(response => {
        if (active) setAccess(response.data.permissions.includes(permission));
      }).catch(() => { if (active) setAccess(false); });
    }
    return () => { active = false; };
  }, [user, permission]);
  if (user === null) return <Navigate to="/login" state={{returnTo: location.pathname}} replace />;
  if (permission && access === null) return <p>正在驗證存取權限…</p>;
  if (permission && !access) return <p>此功能尚未啟用或此身分沒有存取權限。</p>;
  return <>{children}</>;
};
