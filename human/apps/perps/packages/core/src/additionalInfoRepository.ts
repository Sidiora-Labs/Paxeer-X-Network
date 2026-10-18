import { Repository } from "./repository";

export class AdditionalInfoRepository {
  constructor(private readonly repository: Repository) {}

  save(address: string, data: any) {
    this.repository.save(address, data);
  }

  getAll(address: string) {
    return this.repository.getAll(address);
  }

  get(address: string, key: string) {
    return this.repository.get(address, key);
  }

  clear(address: string) {
    this.repository.clear(address);
  }

  remove(address: string, key: string) {
    this.repository.remove(address, key);
  }
}
