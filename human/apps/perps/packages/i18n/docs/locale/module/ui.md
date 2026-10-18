# ui.ts

## ui.ts responsibility

Provides UI-specific copy: empty state description, message center tooltip, pagination labels, and date picker label. Used by shared UI components (tables, pickers, message center).

## ui.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| ui | object | Key-value map | Keys under "ui.*" |
| UI | type | typeof ui | Type export |

## ui.ts keys

| Key | Purpose |
|-----|---------|
| ui.empty.description | No results found |
| ui.messageCenter.tooltip | Check recent announcements |
| ui.pagination.morePages | More pages |
| ui.pagination.rowsPerPage | Rows per page |
| ui.picker.selectDate | Select Date |

## ui.ts Example

```typescript
t("ui.empty.description");
t("ui.pagination.rowsPerPage");
```
