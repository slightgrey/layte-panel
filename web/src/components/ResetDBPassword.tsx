import { KeyRound } from "lucide-react";
import { useState } from "react";
import { useResetDBPassword } from "@/api/databases";
import type { DatabaseRow } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Notice } from "@/components/ui/notice";
import { FormError } from "@/pages/AuthShell";

/**
 * "Reset password" for a database. WordPress databases ask first: the new
 * password is written into the site's wp-config.php in the same step, and
 * both are put back if the site stops answering.
 */
export function ResetDBPassword({
  db,
  onSecrets,
  variant = "outline",
  label = "Reset password",
}: {
  db: DatabaseRow;
  onSecrets: (s: Record<string, string>) => void;
  variant?: "outline" | "ghost";
  label?: string;
}) {
  const reset = useResetDBPassword();
  const [confirm, setConfirm] = useState(false);
  const run = () =>
    reset.mutate(db.name, {
      onSuccess: (r) => {
        setConfirm(false);
        onSecrets(r.secrets);
      },
    });
  return (
    <>
      <Button
        variant={variant}
        size={variant === "ghost" ? "sm" : "default"}
        disabled={reset.isPending}
        title={db.wordpress ? "New password, also written into wp-config.php" : "Generate a new password"}
        onClick={() => (db.wordpress ? setConfirm(true) : run())}
      >
        <KeyRound /> {reset.isPending ? "Changing…" : label}
      </Button>
      {!confirm && reset.error && <span className="self-center text-xs text-destructive">{reset.error.message}</span>}
      {confirm && (
        <Dialog open onClose={() => !reset.isPending && setConfirm(false)} title={`New password for ${db.name}`} locked={reset.isPending}>
          <div className="flex flex-col gap-4">
            <p className="text-sm text-muted-foreground">
              Layte Panel sets a new random password for the MySQL user{" "}
              <code className="font-mono">{db.user || "of this database"}</code> and writes it into{" "}
              {db.site_domain ? (
                <>
                  <strong>{db.site_domain}</strong>'s
                </>
              ) : (
                "the site's"
              )}{" "}
              <code className="font-mono">wp-config.php</code> in the same step.
            </p>
            <Notice>
              The site is checked right after the change. If it doesn't answer exactly as before, the old password and the old
              wp-config.php are put back automatically.
            </Notice>
            <FormError message={reset.error?.message} />
            <div className="flex justify-end gap-2">
              <Button type="button" variant="outline" disabled={reset.isPending} onClick={() => setConfirm(false)}>
                Cancel
              </Button>
              <Button disabled={reset.isPending} onClick={run}>
                <KeyRound /> {reset.isPending ? "Changing…" : "Change password"}
              </Button>
            </div>
          </div>
        </Dialog>
      )}
    </>
  );
}
