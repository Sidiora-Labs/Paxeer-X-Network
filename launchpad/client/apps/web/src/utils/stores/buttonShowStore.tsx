import { create } from 'zustand'

export const useButtonShowStore = create((set, get) => ({
    show: false,
    getButtonShowState: () => (get() as any).show,
    setButtonShowState: (show: boolean) => {
        set({ show });
    }
}));