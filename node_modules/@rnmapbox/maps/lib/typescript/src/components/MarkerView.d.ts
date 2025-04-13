import React from 'react';
import { type ViewProps } from 'react-native';
import { type Position } from '../types/Position';
type Props = ViewProps & {
    /**
     * The center point (specified as a map coordinate) of the marker.
     */
    coordinate: Position;
    /**
     * Any coordinate between (0, 0) and (1, 1), where (0, 0) is the top-left corner of
     * the view, and (1, 1) is the bottom-right corner. Defaults to the center at (0.5, 0.5).
     */
    anchor: {
        x: number;
        y: number;
    };
    /**
     * @v10
     *
     * Whether or not nearby markers on the map should all be displayed. If false, adjacent
     * markers will 'collapse' and only one will be shown. Defaults to false.
     */
    allowOverlap: boolean;
    /**
     * Whether or not nearby markers on the map should be hidden if close to a
     * UserLocation puck. Defaults to false.
     */
    allowOverlapWithPuck: boolean;
    isSelected: boolean;
    /**
     * One or more valid React Native views.
     */
    children: React.ReactElement;
};
/**
 * MarkerView represents an interactive React Native marker on the map.
 *
 * If you have static views, consider using PointAnnotation or SymbolLayer to display
 * an image, as they'll offer much better performance. Mapbox suggests using this
 * component for a maximum of around 100 views displayed at one time.
 *
 * This is implemented with view annotations on [Android](https://docs.mapbox.com/android/maps/guides/annotations/view-annotations/)
 * and [iOS](https://docs.mapbox.com/ios/maps/guides/annotations/view-annotations).
 *
 * This component has no dedicated `onPress` method. Instead, you should handle gestures
 * with the React views passed in as `children`.
 */
declare class MarkerView extends React.PureComponent<Props> {
    static defaultProps: Partial<Props>;
    static lastId: number;
    __idForPointAnnotation?: string;
    _idForPointAnnotation(): string;
    _getCoordinate(coordinate: Position): string | undefined;
    render(): React.JSX.Element;
}
export default MarkerView;
//# sourceMappingURL=MarkerView.d.ts.map