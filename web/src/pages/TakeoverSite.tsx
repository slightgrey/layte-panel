import { TriangleAlert, Wrench } from "lucide-react";
import { type FormEvent, useEffect, useState } from "react";
import { useSearchParams } from "react-router";
import { useTakeover, useTakeoverPreview } from "@/api/sites";
import type { Site } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Notice } from "@/components/ui/notice";
import { cn } from "@/lib/utils";
import { FormError } from "./AuthShell";

/** "Let Layte manage nginx" button + dialog for adopted sites: shows the diff, then starts the job. */
export function TakeoverSite({ site }: { site: Site }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        <Wrench /> Let Layte manage nginx…
      </Button>
      {open && <TakeoverDialog site={site} onClose={() => setOpen(false)} />}
    </>
  );
}

function Diff({ title, lines }: { title: string; lines: string[] }) {
  return (
    <div>
      <p className="eyebrow mb-1.5">{title}</p>
      <pre className="max-h-72 overflow-auto rounded-[12px] bg-background p-3 font-mono text-xs leading-relaxed shadow-inset">
        {lines.map((l, i) => (
          <div
            key={i}
            className={cn(l.startsWith("+ ") && "text-success", l.startsWith("- ") && "text-destructive", l.startsWith("  ") && "text-muted-foreground")}
          >
            {l}
          </div>
        ))}
      </pre>
    </div>
  );
}

function TakeoverDialog({ site, onClose }: { site: Site; onClose: () => void }) {
  const preview = useTakeoverPreview(site.id);
  const takeover = useTakeover(site.id);
  const [, setParams] = useSearchParams();
  const [email, setEmail] = useState("");
  const [understood, setUnderstood] = useState(false);
  useEffect(() => {
    if (preview.data?.email && !email) setEmail(preview.data.email);
  }, [preview.data, email]);

  const p = preview.data;
  const blocked = Boolean(p?.blockers?.length);

  return (
    <Dialog open onClose={onClose} title={`Let Layte Panel manage nginx for ${site.domain}`} wide>
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
              takeover.mutate(
                { vhost_hash: p.vhost_hash, email: email.trim() },
                {
                  onSuccess: (r) => {
                    onClose();
                    setParams({ job: r.job_id }, { replace: true });
                  },
                },
              );
            }}
          >
            <p className="text-sm text-muted-foreground">
              Layte Panel replaces this site's nginx config and PHP-FPM pool with its own, so you can switch PHP versions,
              change PHP settings and turn on the page cache here. Files stay in{" "}
              <code className="font-mono">{p.web_root}</code>. Every step is undone automatically if a later one fails or
              the site answers differently afterwards.
            </p>

            {blocked && (
              <Notice tone="danger">
                <div>
                  Layte Panel can't take over this config yet:
                  <ul className="mt-1 list-disc pl-5">
                    {p.blockers!.map((b) => (
                      <li key={b}>{b}</li>
                    ))}
                  </ul>
                </div>
              </Notice>
            )}

            {!!p.warnings?.length && (
              <Notice tone="warning" icon={TriangleAlert}>
                <div>
                  Your config has rules Layte's doesn't. They <strong>won't be kept</strong>:
                  <ul className="mt-1 list-disc pl-5 font-mono text-xs">
                    {p.warnings.map((w) => (
                      <li key={w}>{w}</li>
                    ))}
                  </ul>
                </div>
              </Notice>
            )}

            <ol className="flex list-decimal flex-col gap-1.5 pl-5 text-sm">
              {p.ssl && (
                <li>
                  Get a Layte certificate from Let's Encrypt (certbot puts a challenge file in the web root, served by your
                  current config). If that fails, nothing is changed. Your current certificate is left alone.
                </li>
              )}
              <li>
                Start a Layte PHP {p.php_version} pool <code className="font-mono">{p.new_pool_file}</code> next to the
                current one.
              </li>
              <li>
                {p.kept_as ? (
                  <>
                    Move your nginx config <code className="font-mono">{p.vhost_file}</code> to{" "}
                    <code className="font-mono">{p.kept_as}</code>
                  </>
                ) : (
                  <>
                    Disable <code className="font-mono">{p.vhost_file}</code> (only the link is removed; the file stays)
                  </>
                )}{" "}
                and use <code className="font-mono">{p.new_vhost_file}</code> instead (one <code>nginx -t</code> and reload).
              </li>
              <li>Check that the site answers exactly as before, then remove the old pool {p.pool_file}.</li>
            </ol>

            <Diff title={`${p.vhost_file} → ${p.new_vhost_file}${p.ssl ? " (HTTPS block uses the new certificate)" : ""}`} lines={p.vhost_diff} />
            <Diff title={`${p.pool_file} → ${p.new_pool_file}`} lines={p.pool_diff} />

            {p.ssl && (
              <Field label="Email for certificate expiry notices" htmlFor="takeover-email">
                <Input id="takeover-email" type="email" required className="max-w-sm" value={email} onChange={(e) => setEmail(e.target.value)} />
              </Field>
            )}

            <label className="flex items-start gap-2 text-sm">
              <input type="checkbox" className="mt-1" checked={understood} onChange={(e) => setUnderstood(e.target.checked)} />
              <span>I've read what changes. From now on Layte Panel rewrites this site's nginx config.</span>
            </label>

            <FormError message={takeover.error?.message} />
            <div className="flex justify-end gap-2">
              <Button type="button" variant="outline" onClick={onClose}>
                Cancel
              </Button>
              <Button type="submit" disabled={blocked || !understood || (p.ssl && !email.trim()) || takeover.isPending}>
                <Wrench /> Let Layte manage nginx
              </Button>
            </div>
          </form>
        )
      )}
    </Dialog>
  );
}
