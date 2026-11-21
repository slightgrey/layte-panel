import { Navigate, Route, Routes } from "react-router";
import { Layout } from "./components/Layout";
import { RequireAuth } from "./components/RequireAuth";
import { DashboardPage } from "./pages/Dashboard";
import { CronPage } from "./pages/Cron";
import { DatabasesPage } from "./pages/Databases";
import { FilesPage } from "./pages/Files";
import { LoginPage } from "./pages/Login";
import { SecurityPage } from "./pages/Security";
import { SiteFilesPage } from "./pages/SiteFiles";
import { LogsPage } from "./pages/Logs";
import { NewSitePage } from "./pages/NewSite";
import { SiteDetailPage } from "./pages/SiteDetail";
import { SitesPage } from "./pages/Sites";
import { SSLPage } from "./pages/SSL";
import { TwoFactorPage } from "./pages/TwoFactor";

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/login/2fa" element={<TwoFactorPage />} />
      <Route element={<RequireAuth />}>
        <Route element={<Layout />}>
          <Route index element={<DashboardPage />} />
          <Route path="sites" element={<SitesPage />} />
          <Route path="sites/new" element={<NewSitePage />} />
          <Route path="sites/:id" element={<SiteDetailPage />} />
          <Route path="ssl" element={<SSLPage />} />
          <Route path="databases" element={<DatabasesPage />} />
          <Route path="cron" element={<CronPage />} />
          <Route path="logs" element={<LogsPage />} />
          <Route path="files" element={<FilesPage />} />
          <Route path="security" element={<SecurityPage />} />
          <Route path="sites/:id/files" element={<SiteFilesPage />} />
        </Route>
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
