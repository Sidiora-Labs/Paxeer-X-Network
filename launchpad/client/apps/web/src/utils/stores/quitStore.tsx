import {create} from 'zustand'

interface QuitStore {
    quit: boolean;
    getQuitState: () => boolean;
    setQuitState: (quit: boolean) => void;
}

export const useQuitStore = create<QuitStore>((set, get) => ({
    quit: false,
    getQuitState: () => get().quit,
    setQuitState: (quit: boolean) => {
        set({ quit });
    }
}));