# avatar.tsx

## avatar.tsx 的职责

Provides Avatar and EVMAvatar components. Avatar uses Radix Avatar primitive with avatarVariants (size: 3xs–xl); EVMAvatar generates an identicon from an EVM address via ethereum-blockies-base64. Used for user or entity avatars across the app.

## avatar.tsx 对外导出内容

| Name           | Type      | Role                    | Description                                 |
| -------------- | --------- | ----------------------- | ------------------------------------------- |
| Avatar         | component | Image/fallback avatar   | ForwardRef with size variant                |
| EVMAvatar      | component | Address-based identicon | Renders blockie from address                |
| AvatarSizeType | type      | Size variant type       | From avatarVariants                         |
| avatarVariants | tv        | Style variants          | Slots: root, image, fallback; size variants |

## Avatar / EVMAvatar Props

| Prop                | Type                       | Required | Default | Description                  |
| ------------------- | -------------------------- | -------- | ------- | ---------------------------- |
| size                | AvatarSizeType             | No       | "sm"    | 3xs, 2xs, xs, sm, md, lg, xl |
| (Avatar)            | AvatarPrimitive.Root props | —        | —       | src, alt, etc.               |
| (EVMAvatar) address | string                     | Yes      | —       | EVM address for blockie      |

## Avatar / EVMAvatar 依赖与调用关系

- **Upstream**: @radix-ui/react-avatar, ethereum-blockies-base64, tv (utils/tv).
- **Downstream**: Exported from avatar/index.ts and main package.

## Avatar / EVMAvatar Example

```tsx
import { Avatar, EVMAvatar } from "@paxeer-network/ui";

<Avatar src="/me.png" alt="Me" size="md" />
<EVMAvatar address="0x1234…" size="lg" />
```
