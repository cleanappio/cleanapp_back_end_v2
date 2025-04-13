import React, { ReactElement } from 'react';
import { Point } from 'geojson';
import { AnimatedPoint } from '../classes';
import { OnPressEvent } from '../types/OnPressEvent';
import { SymbolLayerStyle } from '../Mapbox';
type Props = {
    id: string;
    animated?: boolean;
    animationDuration?: number;
    animationEasingFunction?: (x: number) => number;
    coordinates: number[];
    onPress?: (event: OnPressEvent) => void;
    children: ReactElement | ReactElement[];
    style?: object;
    icon?: string | number | object;
};
type AnnotationState = {
    shape: Point | AnimatedPoint | null;
};
declare class Annotation extends React.Component<Props, AnnotationState> {
    static defaultProps: {
        animated: boolean;
        animationDuration: number;
        animationEasingFunction: import("react-native").EasingFunction;
    };
    constructor(props: Props);
    componentDidUpdate(prevProps: Props): void;
    onPress(event: OnPressEvent): void;
    _getShapeFromProps(props?: Partial<Props>): Point;
    get symbolStyle(): SymbolLayerStyle | undefined;
    render(): React.JSX.Element | null;
}
export default Annotation;
//# sourceMappingURL=Annotation.d.ts.map