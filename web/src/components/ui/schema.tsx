import { createContext, useContext } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../../lib/utils";
import { Input } from "./input";
import type { UINode } from "../../types";

// SchemaRenderer renders a plugin-provided UI assembly tree. The vocabulary
// is Vuetify's: each node carries a Vuetify component name plus free props;
// form fields (VSwitch, VTextField, VTextarea, VSelect, VCheckbox) bind to
// the plugin's config values via props.model. Unknown component names
// render as a warning box.

interface FormContextValue {
  values: Record<string, unknown>;
  onChange: (model: string, value: unknown) => void;
  disabled: boolean;
}

const FormContext = createContext<FormContextValue>({
  values: {},
  onChange: () => {},
  disabled: false,
});

export function SchemaRenderer({
  node,
  values,
  onChange,
  disabled = false,
}: {
  node: UINode;
  values?: Record<string, unknown>;
  onChange?: (model: string, value: unknown) => void;
  disabled?: boolean;
}) {
  return (
    <FormContext.Provider
      value={{
        values: values || {},
        onChange: onChange || (() => {}),
        disabled,
      }}
    >
      <RenderNode node={node} />
    </FormContext.Provider>
  );
}

const FIELD_COMPONENTS = new Set([
  "VSwitch",
  "VCheckbox",
  "VCheckboxBtn",
  "VTextField",
  "VTextarea",
  "VSelect",
]);

function RenderNode({ node }: { node: UINode }) {
  const props = node.props || {};
  const content = (Array.isArray(node.content) ? node.content : []).map((c, i) => (
    <RenderNode key={i} node={c} />
  ));

  switch (node.component) {
    case "VForm":
      return <div className="space-y-3">{content}</div>;
    case "VRow":
      return <div className="flex flex-wrap gap-3">{content}</div>;
    case "VCol": {
      const cols = Number(props.cols ?? props.md ?? 12);
      // Vuetify allows "auto" for cols — Number("auto") is NaN and would
      // produce invalid CSS; treat any non-finite value as full width.
      const width =
        !Number.isFinite(cols) || cols <= 0 || cols > 12
          ? "100%"
          : `calc(${(cols / 12) * 100}% - 0.75rem)`;
      return <div style={{ width, minWidth: "10rem" }}>{content}</div>;
    }
    case "VDivider":
      return <div className="border-t border-zinc-800 my-1" />;
    case "VCard":
      return (
        <div className="rounded-lg bg-zinc-800/50 p-3 space-y-2">
          {typeof props.title === "string" && props.title !== "" && (
            <p className="text-sm font-medium">{props.title}</p>
          )}
          {typeof props.text === "string" && props.text !== "" && (
            <p className="text-xs text-zinc-400">{props.text}</p>
          )}
          {content.length > 0 && content}
        </div>
      );
    case "VCardTitle":
      return <p className="text-sm font-medium">{String(props.text ?? "")}</p>;
    case "VCardText":
      return <p className="text-xs text-zinc-400">{String(props.text ?? "")}</p>;
    case "VAlert":
      return <SchemaAlert type={String(props.type ?? "info")} text={String(props.text ?? "")} />;
    case "VTable": {
      const columns = Array.isArray(props.columns) ? (props.columns as string[]) : [];
      const rows = (Array.isArray(props.rows) ? (props.rows as unknown[]) : []).filter(
        (r): r is unknown[] => Array.isArray(r),
      );
      return (
        <table className="w-full text-sm">
          <thead>
            <tr>
              {columns.map((c, i) => (
                <th key={i} className="text-left text-xs text-zinc-500 font-normal py-1">
                  {c}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row, i) => (
              <tr key={i} className="border-t border-zinc-800">
                {row.map((cell, j) => (
                  <td key={j} className="py-1 pr-2">
                    {String(cell)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      );
    }
    default:
      if (FIELD_COMPONENTS.has(node.component)) {
        return <SchemaField component={node.component} props={props} />;
      }
      return (
        <div className="text-xs text-yellow-500/80 rounded-lg border border-yellow-600/30 bg-yellow-600/10 px-3 py-2">
          unknown component "{node.component}"
        </div>
      );
  }
}

// DANGEROUS_MODELS are prototype-chain member names: reading them via
// values[model] would hit the object prototype instead of the config, and
// onChange would persist bogus keys into the plugin config.
const DANGEROUS_MODELS = new Set(["__proto__", "constructor", "prototype"]);

function SchemaField({ component, props }: { component: string; props: Record<string, unknown> }) {
  const { t } = useTranslation();
  const { values, onChange, disabled } = useContext(FormContext);
  const model = String(props.model ?? "");
  const label = String(props.label ?? model);
  // Unbound fields (no model) are display-only: controls are disabled so
  // an empty-string key can never be persisted into the config.
  const isUnbound = model === "";
  const value =
    Object.prototype.hasOwnProperty.call(values, model) && !DANGEROUS_MODELS.has(model)
      ? values[model]
      : undefined;
  const items = Array.isArray(props.items)
    ? (props.items as { title: string; value?: unknown }[])
    : [];
  const multiple = !!props.multiple;

  if (DANGEROUS_MODELS.has(model)) {
    return (
      <div className="text-xs text-yellow-500/80 rounded-lg border border-yellow-600/30 bg-yellow-600/10 px-3 py-2">
        {t("plugins.fieldNoModel")}
      </div>
    );
  }

  const input = (() => {
    switch (component) {
      case "VSwitch":
        return (
          <button
            type="button"
            role="switch"
            aria-checked={!!value}
            onClick={() => onChange(model, !value)}
            disabled={disabled || isUnbound}
            className={`relative w-10 h-5 rounded-full transition-colors cursor-pointer disabled:opacity-50 ${
              value ? "bg-green-600" : "bg-zinc-700"
            }`}
          >
            <span
              className={`absolute top-0.5 left-0.5 w-4 h-4 rounded-full bg-white transition-transform ${
                value ? "translate-x-5" : ""
              }`}
            />
          </button>
        );
      case "VCheckbox":
      case "VCheckboxBtn":
        return (
          <label className="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer">
            <input
              type="checkbox"
              checked={!!value}
              onChange={(e) => onChange(model, e.target.checked)}
              disabled={disabled || isUnbound}
              className="accent-green-600 w-4 h-4"
            />
            {label}
          </label>
        );
      case "VTextarea":
        return (
          <textarea
            value={String(value ?? "")}
            onChange={(e) => onChange(model, e.target.value)}
            placeholder={String(props.placeholder ?? "")}
            disabled={disabled || isUnbound}
            rows={3}
            className="w-full rounded-lg border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-green-500 disabled:opacity-50"
          />
        );
      case "VSelect":
        if (multiple) {
          const rawSelected = Array.isArray(value) ? (value as unknown[]) : [];
          const selected = rawSelected.map(String);
          return (
            <div className="space-y-1">
              {items.map((item, i) => {
                const itemVal = String(item.value ?? item.title ?? "");
                const checked = selected.includes(itemVal);
                return (
                  <label
                    key={i}
                    className="flex items-center gap-2 text-sm text-zinc-300 cursor-pointer"
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={(e) => {
                        const next = e.target.checked
                          ? [...rawSelected, itemVal]
                          : rawSelected.filter((v) => String(v) !== itemVal);
                        onChange(model, next);
                      }}
                      disabled={disabled || isUnbound}
                      className="accent-green-600 w-4 h-4"
                    />
                    {item.title}
                  </label>
                );
              })}
            </div>
          );
        }
        return (
          <select
            value={String(value ?? "")}
            onChange={(e) => {
              // Restore the original item value type (number/boolean)
              // instead of persisting the string the DOM always yields.
              const original = items.find(
                (it) => String(it.value ?? it.title ?? "") === e.target.value,
              );
              onChange(model, original ? original.value : e.target.value);
            }}
            disabled={disabled || isUnbound}
            className="w-full rounded-lg border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-white focus:outline-none focus:border-green-500 disabled:opacity-50"
          >
            <option value="">—</option>
            {items.map((item, i) => (
              <option key={i} value={String(item.value ?? item.title ?? "")}>
                {item.title}
              </option>
            ))}
          </select>
        );
      default:
        return (
          <Input
            type={props.type === "number" ? "number" : "text"}
            value={String(value ?? "")}
            onChange={(e) =>
              onChange(
                model,
                props.type === "number" && e.target.value !== ""
                  ? Number(e.target.value)
                  : e.target.value,
              )
            }
            placeholder={String(props.placeholder ?? "")}
            disabled={disabled || isUnbound}
          />
        );
    }
  })();

  return (
    <div className="space-y-1 w-full">
      {component !== "VCheckbox" && component !== "VCheckboxBtn" && (
        <p className="text-xs text-zinc-400">{label}</p>
      )}
      {input}
      {typeof props.help === "string" && props.help !== "" && (
        <p className="text-xs text-zinc-600">{props.help}</p>
      )}
      {model === "" && <p className="text-[10px] text-yellow-600">{t("plugins.fieldNoModel")}</p>}
    </div>
  );
}

function SchemaAlert({ type, text }: { type: string; text: string }) {
  const styles: Record<string, string> = {
    info: "bg-blue-600/15 text-blue-400 border-blue-600/30",
    // Vuetify's standard value is "warning"; "warn" kept as an alias.
    warn: "bg-yellow-600/15 text-yellow-400 border-yellow-600/30",
    warning: "bg-yellow-600/15 text-yellow-400 border-yellow-600/30",
    error: "bg-red-600/15 text-red-400 border-red-600/30",
    success: "bg-green-600/15 text-green-400 border-green-600/30",
  };
  return (
    <div className={cn("text-xs rounded-lg border px-3 py-2", styles[type] || styles.info)}>
      {text}
    </div>
  );
}
