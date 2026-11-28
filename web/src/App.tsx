import { Navigate, Route, Routes } from "react-router";
import { Layout } from "./components/Layout";
import { RequireAdmin, RequireAuth } from "./components/RequireAuth";
import { DashboardPage } from "./pages/Dashboard";
import { CronPage } from "./pages/Cron";
import { DatabasesPage } from "./pages/Databases";
import { FilesPage } from "./pages/Files";
import { KeysPage } from "./pages/Keys";
import { LoginPage } from "./pages/Login";
import { SecurityPage } from "./pages/Security";
import { SiteFilesPage } from "./pages/SiteFiles";
import { LogsPage } from "./pages/Logs";
import { NewSitePage } from "./pages/NewSite";
import { SiteCronPage } from "./pages/SiteCron";
import { SiteDatabasePage } from "./pages/SiteDatabase";
import { SiteDetailPage } from "./pages/SiteDetail";
import { SiteLogsPage } from "./pages/SiteLogs";
import { SiteSSLPage } from "./pages/SiteSSL";
import { SiteRedirectsPage } from "./pages/SiteRedirects";
import { SiteWPDebugPage } from "./pages/SiteWPDebug";
import { SystemDiskPage } from "./pages/SystemDisk";
import { SystemLoadPage } from "./pages/SystemLoad";
import { SystemMemoryPage } from "./pages/SystemMemory";
import { SystemServicesPage } from "./pages/SystemServices";
import { UpdatesPage } from "./pages/Updates";
import { SitesPage } from "./pages/Sites";
import { SSLPage } from "./pages/SSL";
import { TwoFactorPage } from "./pages/TwoFactor";
import { TwoFactorSetupPage } from "./pages/TwoFactorSetup";
import { SetupLinkPage } from "./pages/SetupLink";
import { UsersPage } from "./pages/Users";
import { AccountPage } from "./pages/Account";

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/login/2fa" element={<TwoFactorPage />} />
      <Route path="/login/2fa/setup" element={<TwoFactorSetupPage />} />
      <Route path="/setup/:token" element={<SetupLinkPage />} />
      <Route element={<RequireAuth />}>
        <Route element={<Layout />}>
          {/* Site accounts: their sites (the server limits them to their own). */}
          <Route path="sites" element={<SitesPage />} />
          <Route path="sites/new" element={<NewSitePage />} />
          <Route path="sites/:id" element={<SiteDetailPage />} />
          <Route path="account" element={<AccountPage />} />
          <Route element={<RequireAdmin />}>
            <Route index element={<DashboardPage />} />
            <Route path="ssl" element={<SSLPage />} />
            <Route path="databases" element={<DatabasesPage />} />
            <Route path="cron" element={<CronPage />} />
            <Route path="logs" element={<LogsPage />} />
            <Route path="files" element={<FilesPage />} />
            <Route path="security" element={<SecurityPage />} />
            <Route path="keys" element={<KeysPage />} />
            <Route path="users" element={<UsersPage />} />
            <Route path="system/load" element={<SystemLoadPage />} />
            <Route path="system/memory" element={<SystemMemoryPage />} />
            <Route path="system/disk" element={<SystemDiskPage />} />
            <Route path="system/services" element={<SystemServicesPage />} />
            <Route path="updates" element={<UpdatesPage />} />
          </Route>
          <Route path="sites/:id/ssl" element={<SiteSSLPage />} />
          <Route path="sites/:id/database" element={<SiteDatabasePage />} />
          <Route path="sites/:id/cron" element={<SiteCronPage />} />
          <Route path="sites/:id/wp-debug" element={<SiteWPDebugPage />} />
          <Route path="sites/:id/redirects" element={<SiteRedirectsPage />} />
          <Route path="sites/:id/logs" element={<SiteLogsPage />} />
          <Route path="sites/:id/files" element={<SiteFilesPage />} />
        </Route>
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
