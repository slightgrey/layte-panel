import { SlidersHorizontal } from "lucide-react";
import { type FormEvent, useState } from "react";
import { useSetPHPSettings } from "@/api/sites";
import type { PHPSettings, Site } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { FormError } from "@/pages/AuthShell";
import { useJobFeedback } from "@/components/JobFeedback";

// Layte's defaults (keep in sync with internal/phpini).
const defaults = { memory_limit: "256M", upload_max: "64M", max_execution_time: 30, max_input_vars: 1000 };

const presets = {
  memory_limit: ["128M", "256M", "512M", "1G"],
  upload_max: ["8M", "32M", "64M", "128M", "256M", "512M"],
  max_execution_time: ["30", "60", "120", "300"],
  max_input_vars: ["1000", "3000", "5000", "10000"],
};

type Form = Record<keyof typeof defaults, string>;

const fromSite = (p: PHPSettings | undefined): Form => ({
  memory_limit: p?.memory_limit || defaults.memory_limit,
  upload_max: p?.upload_max || defaults.upload_max,
  max_execution_time: String(p?.max_execution_time || defaults.max_execution_time),
  max_input_vars: String(p?.max_input_vars || defaults.max_input_vars),
});

/** Sends only values that differ from the defaults, so defaults stay "unset". */
function toSettings(f: Form): PHPSettings {
  const size = (v: string) => v.trim().toUpperCase();
  const out: PHPSettings = {};
  if (size(f.memory_limit) !== defaults.memory_limit) out.memory_limit = size(f.memory_limit);
  if (size(f.upload_max) !== defaults.upload_max) out.upload_max = size(f.upload_max);
  const exec = Number(f.max_execution_time);
  if (exec !== defaults.max_execution_time) out.max_execution_time = exec;
  const vars = Number(f.max_input_vars);
  if (vars !== defaults.max_input_vars) out.max_input_vars = vars;
  return out;
}

/** "PHP settings" button + dialog: a few php.ini values for one site (simple mode). */
export function PHPSettingsButton({ site }: { site: Site }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button size="sm" variant="outline" onClick={() => setOpen(true)}>
        <SlidersHorizontal /> PHP settings
      </Button>
      {open && <PHPSettingsDialog site={site} onClose={() => setOpen(false)} />}
    </>
  );
}

function PHPSettingsDialog({ site, onClose }: { site: Site; onClose: () => void }) {
  const set = useSetPHPSettings(site.id);
  const job = useJobFeedback(`php-settings-${site.id}`, "Saving PHP settings");
  const [form, setForm] = useState<Form>(() => fromSite(site.php_settings));
  const [error, setError] = useState<string | null>(null);
  const field = (key: keyof Form) => ({
    id: `php-${key}`,
    list: `php-${key}-presets`,
    value: form[key],
    onChange: (e: React.ChangeEvent<HTMLInputElement>) => setForm({ ...form, [key]: e.target.value }),
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    for (const key of ["memory_limit", "upload_max"] as const) {
      if (!/^\s*[1-9][0-9]{0,4}\s*[mMgG]\s*$/.test(form[key])) return setError(`Use a size like 256M or 1G.`);
    }
    for (const key of ["max_execution_time", "max_input_vars"] as const) {
      if (!/^\s*[1-9][0-9]*\s*$/.test(form[key])) return setError("Use a whole number.");
    }
    set.mutate(toSettings(form), {
      onSuccess: (r) => {
        onClose();
        job.start(r);
      },
    });
  };

  return (
    <Dialog open onClose={onClose} title={`PHP settings · ${site.domain}`}>
      <form onSubmit={submit} className="flex flex-col gap-4">
        <p className="text-sm text-muted-foreground">
          Pick a value or type your own. Saved in this site's PHP-FPM pool (PHP {site.php_version}), checked with php-fpm -t and put
          back if anything fails.
        </p>
        <Field label="Memory limit" htmlFor="php-memory_limit" hint="memory_limit · 32M to 4G">
          <Input {...field("memory_limit")} placeholder={defaults.memory_limit} />
        </Field>
        <Field label="Max upload size" htmlFor="php-upload_max" hint="upload_max_filesize and post_max_size · 1M to 2G · nginx allows the same size">
          <Input {...field("upload_max")} placeholder={defaults.upload_max} />
        </Field>
        <Field label="Max execution time (seconds)" htmlFor="php-max_execution_time" hint="max_execution_time · 1 to 3600">
          <Input {...field("max_execution_time")} inputMode="numeric" placeholder={String(defaults.max_execution_time)} />
        </Field>
        <Field label="Max input vars" htmlFor="php-max_input_vars" hint="max_input_vars · 100 to 100000 · big menus and WooCommerce variations need more">
          <Input {...field("max_input_vars")} inputMode="numeric" placeholder={String(defaults.max_input_vars)} />
        </Field>
        {Object.entries(presets).map(([key, values]) => (
          <datalist key={key} id={`php-${key}-presets`}>
            {values.map((v) => (
              <option key={v} value={v} />
            ))}
          </datalist>
        ))}
        <FormError message={error ?? set.error?.message} />
        <div className="flex justify-between gap-2">
          <Button type="button" variant="ghost" onClick={() => setForm(fromSite(undefined))}>
            Reset to defaults
          </Button>
          <div className="flex gap-2">
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={set.isPending}>
              Save
            </Button>
          </div>
        </div>
      </form>
    </Dialog>
  );
}
