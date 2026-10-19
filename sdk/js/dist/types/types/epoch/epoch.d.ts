import type { Duration } from "../google/protobuf/duration";
export interface Epoch {
    genesis_time?: Date;
    epoch_duration?: Duration;
    current_epoch: number;
    current_epoch_start_time?: Date;
    current_epoch_height: number;
}
