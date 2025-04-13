/// <reference types="mapbox-gl" />
import { Component, ContextType } from 'react';
import { CameraProps, CameraStop, CameraRef } from '../../components/Camera';
import { Position } from '../../types/Position';
import MapContext from '../MapContext';
declare class Camera extends Component<Pick<CameraProps, 'centerCoordinate' | 'zoomLevel' | 'minZoomLevel' | 'maxZoomLevel'>> implements Omit<CameraRef, 'setCamera'> {
    context: ContextType<typeof MapContext>;
    static contextType: import("react").Context<{
        map?: import("mapbox-gl").Map | undefined;
    }>;
    static UserTrackingModes: never[];
    componentDidMount(): void;
    fitBounds(northEastCoordinates: Position, southWestCoordinates: Position, padding?: number | number[], animationDuration?: number): void;
    flyTo(centerCoordinate: Position, animationDuration?: number): void;
    moveTo(centerCoordinate: Position, animationDuration?: number): void;
    zoomTo(zoomLevel: number, animationDuration?: number): void;
    setCamera(props: CameraStop): void;
    render(): import("react").JSX.Element;
}
export { Camera };
export default Camera;
//# sourceMappingURL=Camera.d.ts.map