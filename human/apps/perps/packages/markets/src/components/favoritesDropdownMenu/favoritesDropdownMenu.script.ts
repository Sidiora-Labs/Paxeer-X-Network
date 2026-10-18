import { useEffect, useState } from "react";
import { FavoriteTab } from "@paxeer-network/hooks";
import { FavoriteInstance } from "../../type";

export type UseFavoritesDropdownMenuScriptOptions = {
  row: any;
  favorite: FavoriteInstance;
};

export type UseFavoritesDropdownMenuScriptReturn = ReturnType<
  typeof useFavoritesDropdownMenuScript
>;

export function useFavoritesDropdownMenuScript(
  options: UseFavoritesDropdownMenuScriptOptions
) {
  const { symbol } = options.row || {};
  const {
    favorites,
    favoriteTabs,
    updateFavoriteTabs,
    updateSymbolFavoriteState,
  } = options.favorite;
  const [open, setOpen] = useState(false);
  const [inputVisible, setInputVisible] = useState(false);
  const [value, setValue] = useState("");
  const [selectedTabs, setSelectedTabs] = useState([] as FavoriteTab[]);

  const hide = () => {
    setOpen(false);
  };

  const showInput = () => {
    setInputVisible(true);
  };

  const hideInput = () => {
    setInputVisible(false);
  };

  const clearState = () => {
    setValue("");
    hideInput();
    setSelectedTabs([]);
  };

  const addTab = () => {
    const newTab = {
      name: value || `WatchList_${favoriteTabs.length}`,
      id: Date.now(),
    };
    updateFavoriteTabs(newTab, { add: true });
    clearState();
  };

  const onCheck = (item: FavoriteTab, checked: boolean) => {
    if (checked) {
      setSelectedTabs(selectedTabs?.filter((tab) => tab.id !== item.id));
    } else {
      setSelectedTabs([...selectedTabs, item]);
    }
    hideInput();
  };

  const confirm = () => {
    updateSymbolFavoriteState(options.row, selectedTabs, false);
    setOpen(false);
  };

  useEffect(() => {
    if (!open) {
      return;
    }

    const find = favorites?.find((item) => item.name === symbol);
    if (find?.tabs?.length) {
      setSelectedTabs(find?.tabs);
    }
  }, [open, favorites, favoriteTabs, symbol]);

  useEffect(() => {
    if (!open) {
      clearState();
    }
  }, [open]);

  return {
    symbol,
    favoriteTabs,
    open,
    onOpenChange: setOpen,
    inputVisible,
    selectedTabs,
    value,
    onValueChange: setValue,
    hide,
    hideInput,
    showInput,
    onCheck,
    addTab,
    confirm,
  };
}
