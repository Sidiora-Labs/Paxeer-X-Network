import { act, type ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

export interface Mounted {
    readonly container: HTMLDivElement;
    query<T extends Element = HTMLElement>(selector: string): T | null;
    all<T extends Element = HTMLElement>(selector: string): T[];
    text(selector: string): string;
    until(check: () => boolean, label?: string): Promise<void>;
    click(selector: string): Promise<void>;
    type(selector: string, value: string): Promise<void>;
    unmount(): Promise<void>;
}

export async function mount(node: ReactNode): Promise<Mounted> {
    const container = document.createElement('div');
    document.body.appendChild(container);
    let root: Root | null = null;
    await act(async () => {
        root = createRoot(container);
        root.render(node);
    });

    const query = <T extends Element = HTMLElement>(selector: string) => container.querySelector<T>(selector);

    return {
        container,
        query,
        all: <T extends Element = HTMLElement>(selector: string) => Array.from(container.querySelectorAll<T>(selector)),
        text(selector: string): string {
            const element = query(selector);
            if (!element) throw new Error(`nothing matches ${selector}`);
            return element.textContent ?? '';
        },
        async until(check: () => boolean, label = 'condition'): Promise<void> {
            for (let attempt = 0; attempt < 300; attempt += 1) {
                if (check()) return;
                await act(async () => {
                    await new Promise((resolve) => setTimeout(resolve, 10));
                });
            }
            throw new Error(`${label} not reached; rendered ${container.textContent ?? ''}`);
        },
        async click(selector: string): Promise<void> {
            const element = query<HTMLElement>(selector);
            if (!element) throw new Error(`nothing matches ${selector}`);
            await act(async () => {
                element.dispatchEvent(new MouseEvent('click', { bubbles: true }));
            });
        },
        async type(selector: string, value: string): Promise<void> {
            const element = query<HTMLInputElement | HTMLSelectElement>(selector);
            if (!element) throw new Error(`nothing matches ${selector}`);
            const prototype = element instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
            const setter = Object.getOwnPropertyDescriptor(prototype, 'value')?.set;
            await act(async () => {
                setter?.call(element, value);
                element.dispatchEvent(new Event(element instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
            });
        },
        async unmount(): Promise<void> {
            await act(async () => {
                root?.unmount();
            });
            container.remove();
        },
    };
}
