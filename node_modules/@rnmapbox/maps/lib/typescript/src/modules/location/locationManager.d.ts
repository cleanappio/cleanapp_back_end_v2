import { NativeEventEmitter, NativeEventSubscription, EmitterSubscription, type AppStateStatus, EventSubscription } from 'react-native';
export declare const LocationModuleEventEmitter: NativeEventEmitter;
/**
 * Location sent by locationManager
 */
export interface Location {
    coords: Coordinates;
    timestamp?: number;
}
/**
 * Coorinates sent by locationManager
 */
interface Coordinates {
    /**
     * The heading (measured in degrees) relative to true north.
     * Heading is used to describe the direction the device is pointing to (the value of the compass).
     * Note that on Android this is incorrectly reporting the course value as mentioned in issue https://github.com/rnmapbox/maps/issues/1213
     * and will be corrected in a future update.
     */
    heading?: number;
    /**
     * The direction in which the device is traveling, measured in degrees and relative to due north.
     * The course refers to the direction the device is actually moving (not the same as heading).
     */
    course?: number;
    /**
     * The instantaneous speed of the device, measured in meters per second.
     */
    speed?: number;
    /**
     * The latitude in degrees.
     */
    latitude: number;
    /**
     * The longitude in degrees.
     */
    longitude: number;
    /**
     * The radius of uncertainty for the location, measured in meters.
     */
    accuracy?: number;
    /**
     * The altitude, measured in meters.
     */
    altitude?: number;
}
/**
 * LocationManager is a singleton, see `locationManager`
 */
export declare class LocationManager {
    _listeners: ((location: Location) => void)[];
    _lastKnownLocation: Location | null;
    _isListening: boolean;
    _requestsAlwaysUse: boolean;
    subscription: EmitterSubscription | EventSubscription | null;
    _appStateListener: NativeEventSubscription;
    _minDisplacement?: number;
    constructor();
    getLastKnownLocation(): Promise<Location | null>;
    addListener(listener: (location: Location) => void): void;
    removeListener(listener: (location: Location) => void): void;
    removeAllListeners(): void;
    _handleAppStateChange(appState: AppStateStatus): void;
    start(displacement?: number): void;
    stop(): void;
    setMinDisplacement(minDisplacement: number): void;
    setRequestsAlwaysUse(requestsAlwaysUse: boolean): void;
    _onUpdate(location: Location): void;
    /**
     * simulates location updates, experimental  [V10, iOS only]
     */
    _simulateHeading(changesPerSecond: number, increment: number): void;
    /**
     * Sets the period at which location events will be sent over the React Native bridge.
     * The default is 0, aka no limit. [V10, iOS only]
     *
     * @example
     * locationManager.setLocationEventThrottle(500);
     *
     * @param {Number} throttleValue event throttle value in ms.
     * @return {void}
     */
    setLocationEventThrottle(throttleValue: number): void;
}
declare const _default: LocationManager;
export default _default;
//# sourceMappingURL=locationManager.d.ts.map