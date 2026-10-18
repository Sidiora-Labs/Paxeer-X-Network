import { create } from 'zustand';

export const useAlertStore = create((set, get) => ({
    clicked: true,
    getClickState: () => (get() as any).clicked,
    setClickState: (clicked: boolean) => {
        set({ clicked });
    }
}));