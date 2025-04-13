import React from 'react';
import { type ColorValue } from 'react-native';
import type { Expression } from '../utils/MapboxStyles';
type Value<T> = T | Expression;
export type Props = {
    /**
     * Android render mode.
     *
     *  - normal: just a circle
     *  - compass: triangle with heading
     *  - gps: large arrow
     *
     * @deprecated use `puckBearing` for source and `bearingImage` for image
     * @platform android
     */
    androidRenderMode?: 'normal' | 'compass' | 'gps';
    /**
     * The bearing of the puck.
     *
     *  - heading: Orients the puck to match the direction in which the device is facing.
     *  - course: Orients the puck to match the direction in which the device is moving.
     */
    puckBearing?: 'heading' | 'course';
    /**
     * Whether the puck rotates to track the bearing source.
     */
    puckBearingEnabled?: boolean;
    /**
     * iOS only. A Boolean value indicating whether the user location annotation may display a permanent heading indicator.
     *
     * @platform ios
     * @deprecated use `puckBearingEnabled={true} puckBrearing="heading"` instead
     */
    iosShowsUserHeadingIndicator?: boolean;
    /**
     * The name of image to use as the top layer for the location indicator. Images component should define this image.
     */
    topImage?: string;
    /**
     * The name of image to use as the middle layer for the location indicator. Images component should define this image.
     */
    bearingImage?: string;
    /**
     * The name of image to use as the background for the location indicator. Images component should define this image.
     */
    shadowImage?: string;
    /**
     * The size of the images, as a scale factor applied to the size of the specified image. Supports expressions based on zoom.
     *
     * @example
     * ["interpolate",["linear"], ["zoom"], 10.0, 1.0, 20.0, 4.0]]
     * @example
     * 2.0
     */
    scale?: Value<number>;
    /**
     * The configration parameters for sonar-like pulsing circle animation shown around the 2D puck.
     */
    pulsing?: {
        /**
         * Flag determining whether the pulsing circle animation.
         */
        isEnabled?: boolean;
        /**
         * The color of the pulsing circle.
         */
        color?: number | ColorValue;
        /**
         * Circle radius configuration for the pulsing circle animation.
         *  - accuracy:  Pulsing circle animates with the `horizontalAccuracy` form the latest puck location.
         *  - number: Pulsing circle should animate with the constant radius.
         */
        radius?: 'accuracy' | number;
    } | 'default';
    /**
     * Whether location icon is visible, defaults to true
     */
    visible?: boolean;
};
/**
 * Renders a puck on the map that shows the device's current location.
 */
declare const LocationPuck: React.MemoExoticComponent<(props: Props) => React.JSX.Element>;
export default LocationPuck;
//# sourceMappingURL=LocationPuck.d.ts.map