import React, { ReactElement } from 'react';
import { type Location } from '../modules/location/locationManager';
export declare enum UserLocationRenderMode {
    Native = "native",
    Normal = "normal"
}
type Props = {
    /**
     * native/android only render mode
     *
     *  - normal: just a circle
     *  - compass: triangle with heading
     *  - gps: large arrow
     *
     * @platform android
     */
    androidRenderMode?: 'normal' | 'compass' | 'gps';
    /**
     * Whether location icon is animated between updates
     */
    animated?: boolean;
    /**
     * Custom location icon of type mapbox-gl-native components
     */
    children?: ReactElement | ReactElement[];
    /**
     * Minimum amount of movement before GPS location is updated in meters
     */
    minDisplacement?: number;
    /**
     * Callback that is triggered on location icon press
     */
    onPress?: () => void;
    /**
     * Callback that is triggered on location update
     */
    onUpdate?: (location: Location) => void;
    /**
     * @deprecated use LocationPuck component instead of UserLocationRenderMode.Native
     * Which render mode to use.
     */
    renderMode?: UserLocationRenderMode;
    /**
     * Request the always location permission, and listen to the location even when the app is in background
     *
     * @platform ios
     */
    requestsAlwaysUse?: boolean;
    /**
     * Show or hide small arrow which indicates direction the device is pointing relative to north.
     */
    showsUserHeadingIndicator?: boolean;
    /**
     * Whether location icon is visible
     */
    visible?: boolean;
};
type UserLocationState = {
    shouldShowUserLocation: false;
    coordinates: number[] | null;
    heading: number | null;
};
declare class UserLocation extends React.Component<Props, UserLocationState> {
    static defaultProps: {
        animated: boolean;
        visible: boolean;
        showsUserHeadingIndicator: boolean;
        requestsAlwaysUse: boolean;
        minDisplacement: number;
        renderMode: UserLocationRenderMode;
    };
    constructor(props: Props);
    _isMounted?: boolean;
    locationManagerRunning?: boolean;
    componentDidMount(): Promise<void>;
    componentDidUpdate(prevProps: Props): Promise<void>;
    componentWillUnmount(): Promise<void>;
    /**
     * Whether to start or stop listening to the locationManager
     *
     * Notice, that listening will start automatically when
     * either `onUpdate` or `visible` are set
     *
     * @async
     * @param {Object} running - Object with key `running` and `boolean` value
     * @return {Promise<void>}
     */
    setLocationManager({ running }: {
        running?: boolean;
    }): Promise<void>;
    /**
     *
     * If locationManager should be running
     *
     * @return {boolean}
     */
    needsLocationManagerRunning(): boolean | undefined;
    _onLocationUpdate(location: Location | null): void;
    _renderNative(): React.JSX.Element;
    render(): React.JSX.Element | null;
}
export default UserLocation;
//# sourceMappingURL=UserLocation.d.ts.map