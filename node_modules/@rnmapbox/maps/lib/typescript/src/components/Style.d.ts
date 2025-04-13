import React from 'react';
import { FilterExpression } from '../utils/MapboxStyles';
type MapboxJSONLayer = {
    type: string;
    id: string;
    paint?: {
        [k: string]: unknown;
    };
    layout?: {
        [k: string]: unknown;
    };
    source?: string;
    minzoom?: number;
    maxzoom?: number;
    filter?: FilterExpression;
    style?: object;
    ['source-layer']: string;
};
type MapboxJSONSource = {
    type: string;
    url?: string;
    tiles?: string[];
    minzoom?: number;
    maxzoom?: number;
    attribution?: string;
    scheme?: string;
    tileSize?: number;
    coordinates?: [
        [
            number,
            number
        ],
        [
            number,
            number
        ],
        [
            number,
            number
        ],
        [
            number,
            number
        ]
    ];
    data?: string | object;
    buffer: number;
    cluster?: boolean;
    clusterRadius?: number;
    clusterMaxZoom?: number;
    clusterProperties?: object;
    tolerance?: number;
    lineMetrics?: boolean;
};
type MapboxJSON = {
    layers?: MapboxJSONLayer[];
    sources?: {
        [key: string]: MapboxJSONSource;
    };
};
type Props = {
    /**
     * A JSON object conforming to the schema described in the Mapbox Style Specification , or a URL to such JSON.
     */
    json: MapboxJSON | URL;
};
/**
 * Style is a component that automatically adds sources / layers to the map using Mapbox GL Style Spec.
 * Only [`sources`](https://docs.mapbox.com/mapbox-gl-js/style-spec/sources) & [`layers`](https://docs.mapbox.com/mapbox-gl-js/style-spec/layers/) are supported.
 * Other fields such as `sprites`, `glyphs` etc. will be ignored. Not all layer / source attributes from the style spec are supported, in general the supported attributes will be mentioned under https://github.com/rnmapbox/maps/tree/main/docs.
 */
declare const Style: (props: Props) => React.JSX.Element;
export default Style;
//# sourceMappingURL=Style.d.ts.map