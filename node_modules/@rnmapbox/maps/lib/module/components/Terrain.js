"use strict";

import React, { memo, useMemo } from 'react';
import { transformStyle } from '../utils/StyleValue';
import RNMBXATerrainNativeComponent from '../specs/RNMBXTerrainNativeComponent';
import { jsx as _jsx } from "react/jsx-runtime";
export const Terrain = /*#__PURE__*/memo(props => {
  let {
    style = {}
  } = props;
  if (props.exaggeration) {
    console.warn(`Terrain: exaggeration property is deprecated pls use style.exaggeration instead!`);
    style = {
      exaggeration: props.exaggeration,
      ...style
    };
  }
  const baseProps = useMemo(() => {
    return {
      ...props,
      reactStyle: transformStyle(style),
      style: undefined
    };
  }, [props, style]);
  return /*#__PURE__*/_jsx(RNMBXATerrainNativeComponent, {
    ...baseProps
  });
});
//# sourceMappingURL=Terrain.js.map