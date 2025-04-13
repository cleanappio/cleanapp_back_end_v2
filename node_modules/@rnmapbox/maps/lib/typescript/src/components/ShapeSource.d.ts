import React from 'react';
import { NativeMethods, NativeSyntheticEvent } from 'react-native';
import { OnPressEvent } from '../types/OnPressEvent';
import { ShapeAnimatorInterface } from '../shapeAnimators';
import AbstractSource from './AbstractSource';
export type Props = {
    /**
     * A string that uniquely identifies the source.
     */
    id: string;
    /**
     * The id refers to en existing source in the style. Does not create a new source.
     */
    existing?: boolean;
    /**
     * An HTTP(S) URL, absolute file URL, or local file URL relative to the current application’s resource bundle.
     */
    url?: string;
    /**
     * The contents of the source. A shape can represent a GeoJSON geometry, a feature, or a feature collection.
     */
    shape?: GeoJSON.GeometryCollection | GeoJSON.Feature | GeoJSON.FeatureCollection | GeoJSON.Geometry | ShapeAnimatorInterface;
    /**
     * Enables clustering on the source for point shapes.
     */
    cluster?: boolean;
    /**
     * Specifies the radius of each cluster if clustering is enabled.
     * A value of 512 produces a radius equal to the width of a tile.
     * The default value is 50.
     */
    clusterRadius?: number;
    /**
     * Specifies the maximum zoom level at which to cluster points if clustering is enabled.
     * Defaults to one zoom level less than the value of maxZoomLevel so that, at the maximum zoom level,
     * the shapes are not clustered.
     */
    clusterMaxZoomLevel?: number;
    /**
     * [`mapbox-gl` (v8) implementation only]
     * Specifies custom properties on the generated clusters if clustering
     * is enabled, aggregating values from clustered points.
     *
     * Has the form `{ "property_name": [operator, map_expression]}`, where
     *  `operator` is a custom reduce expression that references a special `["accumulated"]` value -
     *   it accumulates the property value from clusters/points the cluster contains
     *  `map_expression` produces the value of a single point
     *
     * Example: `{ "resultingSum": [["+", ["accumulated"], ["get", "resultingSum"]], ["get", "scalerank"]] }`
     *
     */
    clusterProperties?: object;
    /**
     * Specifies the maximum zoom level at which to create vector tiles.
     * A greater value produces greater detail at high zoom levels.
     * The default value is 18.
     */
    maxZoomLevel?: number;
    /**
     * Specifies the size of the tile buffer on each side.
     * A value of 0 produces no buffer. A value of 512 produces a buffer as wide as the tile itself.
     * Larger values produce fewer rendering artifacts near tile edges and slower performance.
     * The default value is 128.
     */
    buffer?: number;
    /**
     * Specifies the Douglas-Peucker simplification tolerance.
     * A greater value produces simpler geometries and improves performance.
     * The default value is 0.375.
     */
    tolerance?: number;
    /**
     * Whether to calculate line distance metrics.
     * This is required for line layers that specify lineGradient values.
     * The default value is false.
     */
    lineMetrics?: boolean;
    /**
     * Source press listener, gets called when a user presses one of the children layers only
     * if that layer has a higher z-index than another source layers
     *
     * @param {Object} event
     * @param {Object[]} event.features - the geojson features that have hit by the press (might be multiple)
     * @param {Object} event.coordinates - the coordinates of the click
     * @param {Object} event.point - the point of the click
     * @return void
     */
    onPress?: (event: OnPressEvent) => void;
    /**
     * Overrides the default touch hitbox(44x44 pixels) for the source layers
     */
    hitbox?: {
        /**
         * `width` of hitbox
         */
        width: number;
        /**
         * `height` of hitbox
         */
        height: number;
    };
    children?: React.ReactElement | React.ReactElement[];
};
declare const ShapeSource_base: {
    new (...args: any[]): {
        _turboModule: import("react-native").TurboModule;
        _preRefMapMethodQueue: {
            method: {
                name: string;
                args: import("../utils").NativeArg[];
            };
            resolver: (value: import("../utils").NativeArg) => void;
        }[];
        _runPendingNativeMethods<RefType>(nativeRef: RefType): Promise<void>;
        _runNativeMethod<RefType_1, ReturnType_1 = import("../utils").NativeArg>(methodName: string, nativeRef: RefType_1 | undefined, args?: import("../utils").NativeArg[]): Promise<ReturnType_1>;
        context: unknown;
        setState<K extends never>(state: {} | ((prevState: Readonly<{}>, props: object) => {} | Pick<{}, K> | null) | Pick<{}, K> | null, callback?: (() => void) | undefined): void;
        forceUpdate(callback?: (() => void) | undefined): void;
        render(): React.ReactNode;
        readonly props: object;
        state: Readonly<{}>;
        refs: {
            [key: string]: React.ReactInstance;
        };
        componentDidMount?(): void;
        shouldComponentUpdate?(nextProps: object, nextState: Readonly<{}>, nextContext: any): boolean;
        componentWillUnmount?(): void;
        componentDidCatch?(error: Error, errorInfo: React.ErrorInfo): void;
        getSnapshotBeforeUpdate?(prevProps: object, prevState: Readonly<{}>): any;
        componentDidUpdate?(prevProps: object, prevState: Readonly<{}>, snapshot?: any): void;
        componentWillMount?(): void;
        UNSAFE_componentWillMount?(): void;
        componentWillReceiveProps?(nextProps: object, nextContext: any): void;
        UNSAFE_componentWillReceiveProps?(nextProps: object, nextContext: any): void;
        componentWillUpdate?(nextProps: object, nextState: Readonly<{}>, nextContext: any): void;
        UNSAFE_componentWillUpdate?(nextProps: object, nextState: Readonly<{}>, nextContext: any): void;
    };
} & {
    new (props: Props & import("../types/BaseProps").BaseProps): AbstractSource<Props, NativeProps>;
    new (props: Props & import("../types/BaseProps").BaseProps, context: any): AbstractSource<Props, NativeProps>;
    contextType?: React.Context<any> | undefined;
};
/**
 * ShapeSource is a map content source that supplies vector shapes to be shown on the map.
 * The shape may be an url or a GeoJSON object
 */
export declare class ShapeSource extends ShapeSource_base {
    static NATIVE_ASSETS_KEY: string;
    static defaultProps: {
        id: any;
    };
    constructor(props: Props);
    _setNativeRef(nativeRef: React.Component<NativeProps> & Readonly<NativeMethods>): void;
    /**
     * Returns the zoom needed to expand the cluster.
     *
     * @example
     * const zoom = await shapeSource.getClusterExpansionZoom(clusterId);
     *
     * @param  {Feature} feature - The feature cluster to expand.
     * @return {number}
     */
    getClusterExpansionZoom(feature: string | GeoJSON.Feature): Promise<number>;
    /**
     * Returns the FeatureCollection from the cluster.
     *
     * @example
     * const collection = await shapeSource.getClusterLeaves(clusterId, limit, offset);
     *
     * @param  {GeoJSON.Feature} feature - The feature cluster to expand.
     * @param  {number} limit - The number of points to return.
     * @param  {number} offset - The amount of points to skip (for pagination).
     * @return {FeatureCollection}
     */
    getClusterLeaves(feature: number | GeoJSON.Feature, limit: number, offset: number): Promise<any>;
    /**
     * Returns the FeatureCollection from the cluster (on the next zoom level).
     *
     * @example
     * const collection = await shapeSource.getClusterChildren(clusterId);
     *
     * @param  {GeoJSON.Feature} feature - The feature cluster to expand.
     * @return {FeatureCollection}
     */
    getClusterChildren(feature: number | GeoJSON.Feature): Promise<any>;
    setNativeProps(props: NativeProps): void;
    _getShape(): string | undefined;
    _decodePayload(payload: OnPressEvent | string): OnPressEvent;
    onPress(event: NativeSyntheticEvent<{
        payload: OnPressEvent;
    }>): void;
    render(): React.JSX.Element;
}
type NativeProps = {
    id: string;
    shape?: string;
};
export {};
//# sourceMappingURL=ShapeSource.d.ts.map