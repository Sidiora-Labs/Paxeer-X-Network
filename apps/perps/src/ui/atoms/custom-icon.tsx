import Image from "next/image";
import { cn } from "@/ui/utils/cn";

interface IconProps {
  className?: string;
  size?: number;
}

function icon(src: string, name: string) {
  const Icon = ({ className, size = 16 }: IconProps) => (
    <Image
      src={src}
      alt={name}
      width={size}
      height={size}
      className={cn("inline-block shrink-0", className)}
    />
  );
  Icon.displayName = name;
  return Icon;
}

// ── Mapped custom icons (keep original SVG colors) ──
export const IcoSearch = icon("/assets/search.svg", "Search");
export const IcoWallet = icon("/assets/wallet_svgrepo.svg", "Wallet");
export const IcoSettings = icon("/assets/setting.svg", "Settings");
export const IcoCopy = icon("/assets/copy.svg", "Copy");
export const IcoStar = icon("/assets/star.svg", "Star");
export const IcoClock = icon("/assets/clock.svg", "Clock");
export const IcoFilter = icon("/assets/filter.svg", "Filter");
export const IcoShare = icon("/assets/share.svg", "Share");
export const IcoTrash = icon("/assets/trash.svg", "Trash");
export const IcoPlus = icon("/assets/plus.svg", "Plus");
export const IcoLock = icon("/assets/lock.svg", "Lock");
export const IcoGraph = icon("/assets/graph.svg", "Graph");
export const IcoExchange = icon("/assets/exchange.svg", "Exchange");
export const IcoBuy = icon("/assets/buy.svg", "Buy");
export const IcoMoney = icon("/assets/money.svg", "Money");
export const IcoTarget = icon("/assets/target.svg", "Target");
export const IcoLink = icon("/assets/link.svg", "Link");
export const IcoPeople = icon("/assets/people.svg", "People");
export const IcoVolume = icon("/assets/volume.svg", "Volume");
export const IcoFlag = icon("/assets/flag.svg", "Flag");
export const IcoFire = icon("/assets/fire.svg", "Fire");
export const IcoWorld = icon("/assets/world.svg", "World");
export const IcoLeave = icon("/assets/leave.svg", "Leave");
export const IcoAlert = icon("/assets/alert.svg", "Alert");
export const IcoWarn = icon("/assets/warn.svg", "Warn");
export const IcoDropdown = icon("/assets/dropdown.svg", "Dropdown");
export const IcoAvatar = icon("/assets/avatar.svg", "Avatar");
export const IcoShield = icon("/assets/case.svg", "Shield");
export const IcoDocument = icon("/assets/Document.svg", "Document");
export const IcoArrowsLeftRight = icon("/assets/ArrowsLeftRight.svg", "ArrowsLeftRight");
export const IcoNextArrow = icon("/assets/nextarrow.svg", "NextArrow");
export const IcoGas = icon("/assets/gas.svg", "Gas");
export const IcoRun = icon("/assets/run.svg", "Run");
export const IcoWidget = icon("/assets/widget.svg", "Widget");
export const IcoDiscord = icon("/assets/Discord.svg", "Discord");
export const IcoX = icon("/assets/X.svg", "X");
export const IcoExclamation = icon("/assets/exclamation.svg", "Exclamation");
export const IcoExit = icon("/assets/exit.svg", "Exit");
export const IcoDownMenu = icon("/assets/downmenu.svg", "DownMenu");
export const IcoDraggable = icon("/assets/draggable.svg", "Draggable");
export const IcoLayer = icon("/assets/layer.svg", "Layer");
export const IcoAi = icon("/assets/ai.svg", "AI");
export const IcoPremium = icon("/assets/premium.svg", "Premium");
export const IcoWind = icon("/assets/wind.svg", "Wind");
export const IcoDocker = icon("/assets/docker.svg", "Docker");
export const IcoSpeaker = icon("/assets/Speaker.svg", "Speaker");
export const IcoPaint = icon("/assets/Paint.svg", "Paint");
export const IcoSet = icon("/assets/set.svg", "Set");
export const IcoCircleChain = icon("/assets/circle_chain.svg", "CircleChain");
export const IcoLinkBlue = icon("/assets/link_blue.svg", "LinkBlue");
export const IcoLinkWhite = icon("/assets/link_white.svg", "LinkWhite");
export const IcoLinkCyan = icon("/assets/link_cyan.svg", "LinkCyan");
export const IcoQuitWhite = icon("/assets/quit_white.svg", "QuitWhite");
export const IcoPlusWhite = icon("/assets/plus_white.svg", "PlusWhite");
export const IcoDropdownWhite = icon("/assets/dropdown_white.svg", "DropdownWhite");
export const IcoAvatarDark = icon("/assets/avatar_dark.svg", "AvatarDark");
export const IcoAlertLarge = icon("/assets/alert_large.svg", "AlertLarge");
