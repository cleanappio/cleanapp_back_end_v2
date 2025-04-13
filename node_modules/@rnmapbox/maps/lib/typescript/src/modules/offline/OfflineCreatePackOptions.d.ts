export type OfflineCreatePackOptionsArgs = {
    name: string;
    styleURL: string;
    bounds: [GeoJSON.Position, GeoJSON.Position];
    minZoom?: number;
    maxZoom?: number;
    metadata?: Record<string, unknown>;
};
declare class OfflineCreatePackOptions {
    readonly name: string;
    readonly styleURL: string;
    readonly bounds: string;
    readonly minZoom: number | undefined;
    readonly maxZoom: number | undefined;
    readonly metadata: string | undefined;
    constructor(options: OfflineCreatePackOptionsArgs);
    _assert(options: OfflineCreatePackOptionsArgs): void;
    _makeLatLngBounds(bounds: [GeoJSON.Position, GeoJSON.Position]): string;
    _makeMetadata(metadata: Record<string, unknown> | undefined): string;
}
export default OfflineCreatePackOptions;
//# sourceMappingURL=OfflineCreatePackOptions.d.ts.map