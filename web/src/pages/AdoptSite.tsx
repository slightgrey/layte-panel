import { ShieldCheck, UserPlus } from "lucide-react";
import { type FormEvent, useEffect, useState } from "react";
import { useAdoptPreview, useAdoptSite } from "@/api/sites";
import type { Site } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Notice } from "@/components/ui/notice";
import { cn } from "@/lib/utils";
import { FormError } from "./AuthShell";
import { useJobFeedback } from "@/components/JobFeedback";

/** "Adopt" button + dialog: shows exactly what changes, then starts the job. */
export function AdoptSite({ site }: { site: Site }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        <UserPlus /> Adopt…
      </Button>
      {open && <AdoptDialog site={site} onClose={() => setOpen(false)} />}
    </>
  );
}

function AdoptDialog({ site, onClose }: { site: Site; onClose: () => void }) {
  const preview = useAdoptPreview(site.id, true);
  const adopt = useAdoptSite(site.id);
  const job = useJobFeedback(`adopt-${site.id}`, `Adopting ${site.domain}`);
  const [username, setUsername] = useState("");
  const [understood, setUnderstood] = useState(false);
  useEffect(() => {
    if (preview.data && !username) setUsername(preview.data.username);
  }, [preview.data, username]);

  const p = preview.data;
  const blocked = Boolean(p?.blockers?.length);

  return (
    <Dialog open onClose={onClose} title={`Adopt ${site.domain}`} wide>
      {preview.isPending ? (
        <Spinner />
      ) : preview.error ? (
        <FormError message={preview.error.message} />
      ) : (
        p && (
          <form
            className="flex flex-col gap-5"
            onSubmit={(e: FormEvent) => {
              e.preventDefault();
              adopt.mutate(
                { username: username.trim(), pool_hash: p.pool_hash },
                {
                  onSuccess: (r) => {
                    onClose();
                    job.start(r);
                  },
                },
              );
            }}
          >
            <p className="text-sm text-muted-foreground">
              Layte Panel starts managing this site where it is. Your nginx config for it is <strong>not changed</strong>.
              Every step is undone automatically if a later one fails or the site answers differently afterwards.
            </p>

            {blocked && (
              <Notice tone="danger">
                <div>
                  This site can't be adopted yet:
                  <ul className="mt-1 list-disc pl-5">
                    {p.blockers!.map((b) => (
                      <li key={b}>{b}</li>
                    ))}
                  </ul>
                </div>
              </Notice>
            )}

            {!!p.unsafe?.length && (
              <Notice tone="warning" icon={ShieldCheck}>
                <div>
                  The site currently runs as <code className="font-mono">{p.old_user}</code>, which Layte Panel must never use
                  for a site:
                  <ul className="mt-1 list-disc pl-5 text-muted-foreground">
                    {p.unsafe.map((u) => (
                      <li key={u}>{u}</li>
                    ))}
                  </ul>
                  That's why it gets its own new user below.
                </div>
              </Notice>
            )}

            <ol className="flex list-decimal flex-col gap-1.5 pl-5 text-sm">
              <li>
                Create the Linux user <code className="font-mono">{username || "…"}</code> (no login), with{" "}
                <code className="font-mono">tmp/</code> and <code className="font-mono">logs/</code> in its home for
                database dumps and cron output.
              </li>
              <li>
                Files in <code className="font-mono">{p.web_root}</code> owned by <code className="font-mono">{p.old_user}</code>{" "}
                (and group <code className="font-mono">{p.old_group}</code>) are given to the new user. Files owned by anyone
                else are left alone.
                {!!p.log_files?.length && (
                  <>
                    {" "}
                    Also: <code className="font-mono">{p.log_files.join(", ")}</code>.
                  </>
                )}
              </li>
              <li>
                PHP {p.php_version} pool <code className="font-mono">[{p.pool_name}]</code> runs as the new user. Only its{" "}
                <code className="font-mono">user</code>/<code className="font-mono">group</code> lines change; the original is
                kept as <code className="font-mono">{p.pool_file}.layte-orig</code>.
              </li>
              <li>Check that the site answers exactly as before.</li>
            </ol>

            <div>
              <p className="eyebrow mb-1.5">{p.pool_file}</p>
              <pre className="max-h-64 overflow-auto rounded-[12px] bg-background p-3 font-mono text-xs leading-relaxed shadow-inset">
                {p.pool_diff.map((l, i) => (
                  <div
                    key={i}
                    className={cn(l.startsWith("+ ") && "text-success", l.startsWith("- ") && "text-destructive", l.startsWith("  ") && "text-muted-foreground")}
                  >
                    {l.replace("<new user>", username || "<new user>")}
                  </div>
                ))}
              </pre>
            </div>

            <Field label="New Linux user" htmlFor="adopt-user" hint="3–32 characters: a-z, 0-9, _ and -, starting with a letter.">
              <Input id="adopt-user" required className="max-w-xs font-mono" value={username} onChange={(e) => setUsername(e.target.value)} />
            </Field>

            <label className="flex items-start gap-2 text-sm">
              <input type="checkbox" className="mt-1" checked={understood} onChange={(e) => setUnderstood(e.target.checked)} />
              <span>I have a backup of this site and I've read what changes.</span>
            </label>

            <FormError message={adopt.error?.message} />
            <div className="flex justify-end gap-2">
              <Button type="button" variant="outline" onClick={onClose}>
                Cancel
              </Button>
              <Button type="submit" disabled={blocked || !understood || !username.trim() || adopt.isPending}>
                <UserPlus /> Adopt site
              </Button>
            </div>
          </form>
        )
      )}
    </Dialog>
  );
}
