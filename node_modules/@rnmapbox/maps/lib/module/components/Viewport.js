"use strict";

import React, { forwardRef, memo, useEffect, useImperativeHandle, useMemo, useRef } from 'react';
import NativeViewport from '../specs/RNMBXViewportNativeComponent';
import RNMBXViewportModule from '../specs/NativeRNMBXViewportModule';
import { NativeCommands } from '../utils/NativeCommands';
import { jsx as _jsx } from "react/jsx-runtime";
/**
 * provides a structured approach to organizing camera management logic into states and transitions between them.
 *
 * At any given time, the viewport is either:
 *  - idle
 *  - in a state (camera is being managed by a ViewportState)
 *  - transitioning between states
 *
 * See [android](https://docs.mapbox.com/android/maps/api/${ANDROID_SDK_VERSION}/mapbox-maps-android/com.mapbox.maps.plugin.viewport/viewport.html),
 * [ios](https://docs.mapbox.com/ios/maps/api/${IOS_SDK_VERSION}/Viewport.html#/s:10MapboxMaps8ViewportC)
 */
export const Viewport = /*#__PURE__*/memo(/*#__PURE__*/forwardRef((props, ref) => {
  const commands = useMemo(() => new NativeCommands(RNMBXViewportModule), []);
  const nativeViewport = useRef(null);
  useEffect(() => {
    if (nativeViewport.current) {
      commands.setNativeRef(nativeViewport.current);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [commands, nativeViewport.current]);
  useImperativeHandle(ref, () => ({
    getState() {
      console.log(' => calling getState');
      return commands.call('getState', []);
    },
    async idle() {
      return commands.call('idle', []);
    },
    transitionTo(state, transition) {
      return commands.call('transitionTo', [state, transition]);
    }
  }));
  const onStatusChangedNative = useMemo(() => {
    const propsOnStatusChanged = props.onStatusChanged;
    if (propsOnStatusChanged != null) {
      return event => {
        propsOnStatusChanged(event.nativeEvent.payload);
      };
    } else {
      return undefined;
    }
  }, [props.onStatusChanged]);
  return /*#__PURE__*/_jsx(RNMBXViewport, {
    ...props,
    hasStatusChanged: props.onStatusChanged != null,
    onStatusChanged: onStatusChangedNative,
    ref: nativeViewport
  });
}));
const RNMBXViewport = NativeViewport;
//# sourceMappingURL=Viewport.js.map