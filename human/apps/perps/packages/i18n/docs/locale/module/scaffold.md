# scaffold.ts

## scaffold.ts responsibility

Provides scaffold/layout copy: announcement campaign, footer (join community, operational, powered by).

## scaffold.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| scaffold | object | Key-value map | Keys under "scaffold.*" |
| Scaffold | type | typeof scaffold | Type export |

## scaffold.ts keys

| Key | Purpose |
|-----|---------|
| scaffold.announcement.campaign | Latest Campaign is coming |
| scaffold.footer.joinCommunity | Join our community |
| scaffold.footer.operational | Operational |
| scaffold.footer.poweredBy | Powered by |

## scaffold.ts Example

```typescript
t("scaffold.footer.poweredBy");
```
