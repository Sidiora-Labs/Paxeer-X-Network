// import { MainNav } from "./nav/main/mainNav";
// import { useMainNavBuilder } from "./nav/main/widgetBuilder";
// import { SideBar } from "./nav/sidebar";
// import { useSideNavBuilder } from "./nav/sidebar/builders/useSideNavBuilder";
// import { SideBarProps } from "./nav/sidebar/sidebar";
// import { installExtension } from "./plugin";
// import { ExtensionPositionEnum } from "./plugin";

// installExtension({
//   name: "main-navbar",
//   scope: ["*"],
//   positions: [ExtensionPositionEnum.MainNav],
//   // @ts-ignore
//   builder: useMainNavBuilder,
//   __isInternal: true,
// })((props: any) => {
//   return <MainNav {...props} />;
// });
//
// installExtension<SideBarProps>({
//   name: "side-navbar",
//   scope: ["*"],
//   positions: [ExtensionPositionEnum.SideNav],
//   builder: useSideNavBuilder,
//   __isInternal: true,
// })(SideBar);

export {};
