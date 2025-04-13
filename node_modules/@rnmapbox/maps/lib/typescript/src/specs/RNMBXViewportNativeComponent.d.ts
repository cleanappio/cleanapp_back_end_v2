import type { HostComponent, ViewProps } from 'react-native';
import { DirectEventHandler } from 'react-native/Libraries/Types/CodegenTypes';
import type { UnsafeMixed } from './codegenUtils';
type OptionalProp<T> = UnsafeMixed<T>;
export type FollowPuckOptionsNative = {
    zoom?: number | 'keep';
    pitch?: number | 'keep';
    bearing?: 'course' | 'heading' | number | 'keep';
    padding?: {
        top?: number;
        left?: number;
        bottom?: number;
        right?: number;
    } | 'keep';
};
export type ViewportStateNative = {
    kind: 'followPuck';
    options?: FollowPuckOptionsNative;
} | {
    kind: 'overview';
};
type ViewportStatus = {
    kind: 'idle';
} | {
    kind: 'transition';
    toState: ViewportStateNative;
    transition: ViewportTransition;
};
type ViewportTransition = {
    kind: 'immediate';
} | {
    kind: 'default';
    maxDurationMs?: number;
};
type ViewportStatusChangeReason = 'TransitionStarted' | 'TransitionSucceeded' | 'IdleRequested' | 'UserInteraction';
export type OnStatusChangedEventPayload = {
    from: ViewportStatus;
    to: ViewportStatus;
    reason: ViewportStatusChangeReason;
};
type OnStatusChangedEventType = {
    type: string;
    payload: string;
};
export type OnStatusChangedEventTypeReal = {
    type: 'statuschanged';
    payload: UnsafeMixed<OnStatusChangedEventPayload>;
};
export interface NativeProps extends ViewProps {
    transitionsToIdleUponUserInteraction?: OptionalProp<boolean>;
    hasStatusChanged: boolean;
    onStatusChanged?: DirectEventHandler<OnStatusChangedEventType>;
}
declare const _default: HostComponent<NativeProps>;
export default _default;
export type NativeViewportReal = HostComponent<Omit<NativeProps, 'onStatusChanged'> & {
    onStatusChanged?: DirectEventHandler<OnStatusChangedEventTypeReal>;
}>;
//# sourceMappingURL=RNMBXViewportNativeComponent.d.ts.map