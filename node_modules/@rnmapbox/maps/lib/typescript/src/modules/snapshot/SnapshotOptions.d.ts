export type SnapshotOptionsArgs = {
    centerCoordinate?: GeoJSON.Position;
    width?: number;
    height?: number;
    zoomLevel?: number;
    pitch?: number;
    heading?: number;
    styleURL?: string;
    writeToDisk?: boolean;
    bounds?: number[][];
    withLogo?: boolean;
};
declare class SnapshotOptions {
    readonly styleURL: string;
    readonly heading: number;
    readonly pitch: number;
    readonly zoomLevel: number;
    readonly width: number;
    readonly height: number;
    readonly writeToDisk: boolean;
    readonly withLogo: boolean;
    readonly centerCoordinate: string | undefined;
    readonly bounds: string | undefined;
    constructor(options?: SnapshotOptionsArgs);
    toJSON(): {
        styleURL: string;
        heading: number;
        pitch: number;
        zoomLevel: number;
        width: number;
        height: number;
        writeToDisk: boolean;
        centerCoordinate: string | undefined;
        bounds: string | undefined;
        withLogo: boolean;
    };
    _createCenterCoordPoint(centerCoordinate: number[]): string;
    _createBoundsCollection(bounds: number[][]): string;
}
export default SnapshotOptions;
//# sourceMappingURL=SnapshotOptions.d.ts.map