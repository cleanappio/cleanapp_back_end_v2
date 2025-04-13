import React from 'react';
import AbstractSource from './AbstractSource';
type Props = {
    /**
     * A string that uniquely identifies the source.
     */
    id: string;
    /**
     * The id refers to en existing source in the style. Does not create a new source.
     */
    existing?: boolean;
    /**
     * A URL to a TileJSON configuration file describing the source’s contents and other metadata.
     */
    url?: string;
    /**
     * An array of tile URL templates. If multiple endpoints are specified, clients may use any combination of endpoints.
     * Example: https://example.com/raster-tiles/{z}/{x}/{y}.png
     */
    tileUrlTemplates?: string[];
    /**
     * An unsigned integer that specifies the minimum zoom level at which to display tiles from the source.
     * The value should be between 0 and 22, inclusive, and less than
     * maxZoomLevel, if specified. The default value for this option is 0.
     */
    minZoomLevel?: number;
    /**
     * An unsigned integer that specifies the maximum zoom level at which to display tiles from the source.
     * The value should be between 0 and 22, inclusive, and less than
     * minZoomLevel, if specified. The default value for this option is 22.
     */
    maxZoomLevel?: number;
    /**
     * Size of the map tiles.
     * Mapbox urls default to 256, all others default to 512.
     */
    tileSize?: number;
    children?: React.ReactElement | React.ReactElement[];
};
type NativeProps = Props;
declare class RasterDemSource extends AbstractSource<Props, NativeProps> {
    static defaultProps: {
        id: any;
    };
    constructor(props: Props);
    render(): React.JSX.Element;
}
export default RasterDemSource;
//# sourceMappingURL=RasterDemSource.d.ts.map