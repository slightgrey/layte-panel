import { Navigate, Outlet, useLocation } from "react-router";
import { useMe } from "@/api/auth";
import { ApiError } from "@/api/client";
import { Spinner } from "./Spinner";

/** Renders child routes only for a fully logged-in user (password + 2FA). */
export function RequireAuth() {
  const me = useMe();
  const location = useLocation();

  if (me.isPending) {
    return (
      <div className="grid min-h-svh place-items-center">
        <Spinner />
      </div>
    );
  }
  if (me.error) {
    if (me.error instanceof ApiError && me.error.code === "totp_required") {
      return <Navigate to="/login/2fa" replace />;
    }
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }
  return <Outlet />;
}
