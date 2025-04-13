"use strict";

import React from 'react';
import { NativeModules } from 'react-native';
import RNMBXCircleLayerNativeComponent from '../specs/RNMBXCircleLayerNativeComponent';
import AbstractLayer from './AbstractLayer';
import { jsx as _jsx } from "react/jsx-runtime";
const Mapbox = NativeModules.RNMBXModule;

// @{codepart-replace-start(LayerPropsCommon.codepart-tsx)}

// @{codepart-replace-end}

/**
 * CircleLayer is a style layer that renders one or more filled circles on the map.
 */
class CircleLayer extends AbstractLayer {
  static defaultProps = {
    sourceID: Mapbox.StyleSource.DefaultSourceID
  };
  render() {
    const props = {
      ...this.props,
      ...this.baseProps,
      sourceLayerID: this.props.sourceLayerID
    };
    return (
      /*#__PURE__*/
      // @ts-expect-error just codegen stuff
      _jsx(RNMBXCircleLayerNativeComponent, {
        ref: this.setNativeLayer,
        ...props
      })
    );
  }
}
export default CircleLayer;
//# sourceMappingURL=CircleLayer.js.map