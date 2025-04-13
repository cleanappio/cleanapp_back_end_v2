"use strict";

import React, { memo } from 'react';
import { processColor } from 'react-native';
import RNMBXNativeUserLocation from '../specs/RNMBXNativeUserLocationNativeComponent';
import { jsx as _jsx } from "react/jsx-runtime";
const defaultProps = {
  visible: true
};

/**
 * Renders a puck on the map that shows the device's current location.
 */
const LocationPuck = /*#__PURE__*/memo(props => {
  const {
    iosShowsUserHeadingIndicator,
    pulsing,
    ...rest
  } = props;
  const nativePulsing = pulsing ? _pulsingToNative(pulsing) : undefined;
  let baseProps = {
    ...defaultProps,
    pulsing: nativePulsing
  };
  if (iosShowsUserHeadingIndicator) {
    console.warn('LocationPuck: iosShowsUserHeadingIndicator is deprecated, use puckBearingEnabled={true} puckBearing="heading" instead');
    baseProps = {
      ...baseProps,
      puckBearingEnabled: true,
      puckBearing: 'heading'
    };
  }
  const actualProps = {
    ...baseProps,
    ...rest
  };
  return /*#__PURE__*/_jsx(RNMBXNativeUserLocation, {
    ...actualProps
  });
});
function _pulsingToNative(pulsing) {
  if (pulsing === 'default') {
    return {
      kind: 'default'
    };
  }
  if (pulsing == null) {
    return undefined;
  }
  const {
    color,
    isEnabled,
    radius
  } = pulsing;
  return {
    color: processColor(color),
    isEnabled,
    radius
  };
}
export default LocationPuck;
//# sourceMappingURL=LocationPuck.js.map