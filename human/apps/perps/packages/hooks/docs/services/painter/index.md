# services/painter — Directory Index

## Directory Responsibility

Canvas-based painter for poster generation: layout config, base paint interface, background/data/QR paint, painter orchestration, resources. Used by `usePoster` to draw shareable posters.

## Files

| File | Language | Summary | Entry symbol(s) | Link |
|------|----------|---------|------------------|------|
| basePaint.ts | TS | Base paint interface, DrawOptions | PosterLayoutConfig, DrawOptions | [basePaint.md](basePaint.md) |
| layout.config.ts | TS | Default layout config | DefaultLayoutConfig | [layout.config.md](layout.config.md) |
| painter.ts | TS | Painter orchestration | (painter) | [painter.md](painter.md) |
| backgroundPaint.ts | TS | Background painting | (paint) | [backgroundPaint.md](backgroundPaint.md) |
| dataPaint.ts | TS | Data painting | (paint) | [dataPaint.md](dataPaint.md) |
| qrPaint.ts | TS | QR code painting | (paint) | [qrPaint.md](qrPaint.md) |
| resource.ts | TS | Resource loading | (resource) | [resource.md](resource.md) |
