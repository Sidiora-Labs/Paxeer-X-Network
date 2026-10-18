import { useTopScript } from "./top.script";
import { Top } from "./top.ui";

export const TopWidget = () => {
    const state = useTopScript();
    return (
        <Top {...state}/>
    );
};
