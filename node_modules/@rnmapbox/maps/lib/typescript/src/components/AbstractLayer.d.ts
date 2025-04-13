import React from 'react';
import { NativeMethods, processColor } from 'react-native';
import { AllLayerStyleProps, FilterExpression } from '../utils/MapboxStyles';
import { StyleValue } from '../utils/StyleValue';
import type { BaseProps } from '../types/BaseProps';
type PropsBase = BaseProps & {
    id: string;
    existing?: boolean;
    sourceID?: string;
    minZoomLevel?: number;
    maxZoomLevel?: number;
    aboveLayerID?: string;
    belowLayerID?: string;
    layerIndex?: number;
    filter?: FilterExpression;
    style?: AllLayerStyleProps;
};
declare class AbstractLayer<PropsType extends PropsBase, NativePropsType> extends React.PureComponent<PropsType> {
    get baseProps(): Omit<PropsType, 'style'> & {
        reactStyle?: {
            [key: string]: StyleValue;
        };
    };
    nativeLayer: (React.Component<NativePropsType> & Readonly<NativeMethods>) | null;
    setNativeLayer: (instance: (React.Component<NativePropsType> & Readonly<NativeMethods>) | null) => void;
    getStyleTypeFormatter(styleType: string): typeof processColor | undefined;
    getStyle(style: AllLayerStyleProps | undefined): {
        [key: string]: StyleValue;
    } | undefined;
    setNativeProps(props: {
        [key: string]: unknown;
    }): void;
}
export default AbstractLayer;
//# sourceMappingURL=AbstractLayer.d.ts.map