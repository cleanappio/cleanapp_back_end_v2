"use strict";

import React, { forwardRef, memo, useImperativeHandle, useRef } from 'react';
import RNMBXLightNativeComponent from '../specs/RNMBXLightNativeComponent';
import { transformStyle } from '../utils/StyleValue';
import nativeRef from '../utils/nativeRef';
import { jsx as _jsx } from "react/jsx-runtime";
/**
 * Light represents the light source for extruded geometries
 */
function Light(props, ref) {
  const {
    style,
    ...propWithoutStyle
  } = props;
  const nativeLightRef = nativeRef(useRef(null));
  useImperativeHandle(ref, () => ({
    setNativeProps(_props) {
      let propsToPass = _props;
      if (_props.style) {
        propsToPass = {
          ..._props,
          reactStyle: transformStyle(_props.style)
        };
      }
      nativeLightRef.current?.setNativeProps(propsToPass);
    }
  }));
  return /*#__PURE__*/_jsx(RNMBXLightNativeComponent
  // @ts-expect-error just codegen stuff
  , {
    ref: nativeLightRef,
    testID: "RNMBXLight",
    ...propWithoutStyle,
    reactStyle: transformStyle(style)
  });
}
export default /*#__PURE__*/memo(/*#__PURE__*/forwardRef(Light));
//# sourceMappingURL=Light.js.map