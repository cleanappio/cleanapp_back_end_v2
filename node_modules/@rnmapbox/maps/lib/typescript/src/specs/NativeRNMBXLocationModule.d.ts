import { TurboModule } from "react-native";
import type { EventEmitter } from 'react-native/Libraries/Types/CodegenTypes';
type LocationEvent = {
    type: string;
    payload: {
        coords: {
            latitude: number;
            longitude: number;
            altitude: number;
            accuracy: number;
            speed: number;
            heading: number;
        };
        timestamp: number;
    };
};
export interface Spec extends TurboModule {
    start(minDisplacement: number): void;
    stop(): void;
    setRequestsAlwaysUse(requestsAlwaysUse: boolean): void;
    setMinDisplacement(minDisplacement: number): void;
    getLastKnownLocation(): Promise<LocationEvent['payload']>;
    simulateHeading(changesPerSecond: number, increment: number): void;
    setLocationEventThrottle(throttle: number): void;
    readonly onLocationUpdate: EventEmitter<LocationEvent>;
}
declare const _default: Spec;
export default _default;
//# sourceMappingURL=NativeRNMBXLocationModule.d.ts.map