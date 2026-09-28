'use client';

import { ACCENT_IDS, DENSITY_IDS, FONT_IDS, SIZE_IDS, THEME_IDS } from './catalogue';
import { useTheme } from './ThemeProvider';
import type { ThemeSelection } from './schema';

interface Option<T extends string> {
    id: T;
    label: string;
    swatch?: string;
}

function OptionGroup<T extends string>({
    name,
    label,
    options,
    value,
    onChange,
}: {
    name: keyof ThemeSelection;
    label: string;
    options: ReadonlyArray<Option<T>>;
    value: T;
    onChange: (next: T) => void;
}) {
    return (
        <div>
            <p className="text-[13px] font-bold text-pax-muted uppercase tracking-[0.06em] mb-3 px-1">{label}</p>
            <div role="radiogroup" aria-label={label} data-group={name} className="flex flex-wrap gap-2">
                {options.map((option) => {
                    const selected = option.id === value;
                    return (
                        <button
                            key={option.id}
                            type="button"
                            role="radio"
                            aria-checked={selected}
                            data-option={option.id}
                            onClick={() => onChange(option.id)}
                            className={`flex items-center gap-2 rounded-xl px-3 py-2 text-sm font-medium press-scale transition-colors ${
                                selected
                                    ? 'bg-pax-accent text-[var(--color-action-on-primary)]'
                                    : 'bg-pax-surface text-pax-light hover:bg-[var(--color-surface-control)]'
                            }`}
                        >
                            {option.swatch && (
                                <span
                                    aria-hidden="true"
                                    className="h-4 w-4 shrink-0 rounded-full"
                                    style={{ background: option.swatch }}
                                />
                            )}
                            {option.label}
                        </button>
                    );
                })}
            </div>
        </div>
    );
}

export function ThemePreview() {
    const { resolved, catalogue, selection } = useTheme();
    return (
        <div
            data-testid="theme-preview"
            data-theme-preview={resolved.id}
            className="rounded-2xl bg-pax-card p-4 space-y-3"
            style={{ boxShadow: 'var(--elevation-menu)' }}
        >
            <div className="flex items-center justify-between">
                <p className="font-display text-lg font-semibold text-pax-off-white">{catalogue.themes[resolved.id].label}</p>
                <span className="rounded-full bg-[var(--color-surface-control)] px-2 py-0.5 text-xs text-pax-subtle">
                    {catalogue.sizes[selection.size].label}
                </span>
            </div>
            <p className="text-sm text-pax-light">Balance</p>
            <p className="font-mono tabular-nums text-xl text-pax-off-white">1,024.50 PAX</p>
            <div className="flex items-center gap-3 text-xs">
                <span className="text-pax-success">+2.4%</span>
                <span className="text-pax-error">-0.8%</span>
                <span className="text-pax-warning">Pending</span>
            </div>
            <button
                type="button"
                className="w-full rounded-xl bg-pax-accent py-2.5 text-sm font-semibold text-[var(--color-action-on-primary)]"
            >
                Send
            </button>
        </div>
    );
}

export function AppearanceSettings() {
    const { selection, setSelection, catalogue } = useTheme();
    const themeOptions: Array<Option<ThemeSelection['theme']>> = [
        { id: 'system', label: 'System' },
        ...THEME_IDS.map((id) => ({
            id,
            label: catalogue.themes[id].label,
            swatch: catalogue.themes[id].colors.surface.base,
        })),
    ];
    return (
        <div className="space-y-5" data-testid="appearance-settings">
            <ThemePreview />
            <OptionGroup
                name="theme"
                label="Colour theme"
                options={themeOptions}
                value={selection.theme}
                onChange={(theme) => setSelection({ theme })}
            />
            <OptionGroup
                name="accent"
                label="Accent"
                options={ACCENT_IDS.map((id) => ({
                    id,
                    label: catalogue.accents[id].label,
                    swatch: catalogue.accents[id].primary,
                }))}
                value={selection.accent}
                onChange={(accent) => setSelection({ accent })}
            />
            <OptionGroup
                name="font"
                label="Font"
                options={FONT_IDS.map((id) => ({ id, label: catalogue.fonts[id].label }))}
                value={selection.font}
                onChange={(font) => setSelection({ font })}
            />
            <OptionGroup
                name="size"
                label="Text size"
                options={SIZE_IDS.map((id) => ({ id, label: catalogue.sizes[id].label }))}
                value={selection.size}
                onChange={(size) => setSelection({ size })}
            />
            <OptionGroup
                name="density"
                label="Density"
                options={DENSITY_IDS.map((id) => ({ id, label: catalogue.densities[id].label }))}
                value={selection.density}
                onChange={(density) => setSelection({ density })}
            />
        </div>
    );
}
