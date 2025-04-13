import React, { ReactNode } from 'react';
import { ViewStyle, ViewProps } from 'react-native';
type Props = Omit<ViewProps, 'style'> & {
    /**
     * String that gets displayed in the default callout.
     */
    title: string;
    /**
     * Style property for the Animated.View wrapper, apply animations to this
     */
    style?: ViewStyle;
    /**
     * Style property for the native RNMBXCallout container, set at your own risk.
     */
    containerStyle?: ViewStyle;
    /**
     * Style property for the content bubble.
     */
    contentStyle?: ViewStyle;
    /**
     * Style property for the triangle tip under the content.
     */
    tipStyle?: ViewStyle;
    /**
     * Style property for the title in the content bubble.
     */
    textStyle?: ViewStyle;
};
/**
 *  Callout that displays information about a selected annotation near the annotation.
 */
declare class Callout extends React.PureComponent<Props> {
    get _containerStyle(): ViewStyle[];
    get _hasChildren(): boolean;
    _renderDefaultCallout(): ReactNode;
    _renderCustomCallout(): ReactNode;
    render(): React.JSX.Element;
}
export default Callout;
//# sourceMappingURL=Callout.d.ts.map