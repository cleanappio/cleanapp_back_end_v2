import OfflineCreatePackOptions from './OfflineCreatePackOptions';
type OfflinePackStatus = {
    name: string;
    state: number;
    percentage: number;
    completedResourceCount: number;
    completedResourceSize: number;
    completedTileSize: number;
    completedTileCount: number;
    requiredResourceCount: number;
};
declare class OfflinePackLegacy {
    private pack;
    private _metadata;
    constructor(pack: OfflineCreatePackOptions);
    get name(): any;
    get bounds(): string;
    get metadata(): any;
    status(): Promise<OfflinePackStatus>;
    resume(): Promise<void>;
    pause(): Promise<void>;
}
export default OfflinePackLegacy;
//# sourceMappingURL=OfflinePackLegacy.d.ts.map