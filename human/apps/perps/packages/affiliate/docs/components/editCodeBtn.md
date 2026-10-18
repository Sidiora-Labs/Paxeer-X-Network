# components/editCodeBtn.tsx

## Responsibility of EditCode

Button that renders EditIcon; used to trigger edit referral code (e.g. in referral codes table). onClick is passed to the icon.

## EditCode Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| size | number | No | Icon size |
| onClick | () => void | No | Click handler |

## Dependencies

- react (FC)
- ../icons/editIcon (EditIcon)

## EditCode Example

```tsx
<EditCode onClick={() => openEditModal(row.code)} />
```
