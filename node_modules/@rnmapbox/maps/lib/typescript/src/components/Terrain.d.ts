import React from 'react';
import type { TerrainLayerStyleProps, Value } from '../utils/MapboxStyles';
import type { BaseProps } from '../types/BaseProps';
type Props = BaseProps & {
    /**
     * Name of a source of raster_dem type to be used for terrain elevation.
     */
    sourceID?: string;
    /**
     * Deprecated, use exaggeration in style instead
     */
    exaggeration?: Value<number, ['zoom']>;
    /**
     * Customizable style attributes
     */
    style?: TerrainLayerStyleProps;
};
export declare const Terrain: React.MemoExoticComponent<(props: Props) => React.JSX.Element>;
export {};
//# sourceMappingURL=Terrain.d.ts.map