"use strict";

import React from 'react';
import { Easing } from 'react-native';
import Animated from '../utils/animated/Animated';
import { AnimatedPoint } from '../classes';
import { SymbolLayer } from './SymbolLayer';
import { jsx as _jsx } from "react/jsx-runtime";
class Annotation extends React.Component {
  static defaultProps = {
    animated: false,
    animationDuration: 1000,
    animationEasingFunction: Easing.linear
  };
  constructor(props) {
    super(props);
    const shape = this._getShapeFromProps(props);
    this.state = {
      shape: props.animated ? new AnimatedPoint(shape) : shape
    };
    this.onPress = this.onPress.bind(this);
  }
  componentDidUpdate(prevProps) {
    if (!Array.isArray(this.props.coordinates)) {
      this.setState({
        shape: null
      });
      return;
    }
    const haveCoordinatesChanged = prevProps.coordinates[0] !== this.props.coordinates[0] || prevProps.coordinates[1] !== this.props.coordinates[1];
    if (prevProps.animated !== this.props.animated || haveCoordinatesChanged && (!this.state.shape || !this.props.animated)) {
      const shape = this._getShapeFromProps(this.props);
      this.setState({
        shape: this.props.animated ? new AnimatedPoint(shape) : shape
      });
    } else if (haveCoordinatesChanged && this.props.animated && this.state.shape) {
      // flush current animations
      this.state.shape.stopAnimation();
      this.state.shape.timing({
        coordinates: this.props.coordinates,
        easing: this.props.animationEasingFunction,
        duration: this.props.animationDuration
      }).start();
    }
  }
  onPress(event) {
    if (this.props.onPress) {
      this.props.onPress(event);
    }
  }
  _getShapeFromProps(props = {}) {
    const lng = props.coordinates?.[0] || 0;
    const lat = props.coordinates?.[1] || 0;
    return {
      type: 'Point',
      coordinates: [lng, lat]
    };
  }
  get symbolStyle() {
    if (!this.props.icon) {
      return undefined;
    }
    return Object.assign({}, this.props.style, {
      iconImage: this.props.icon
    });
  }
  render() {
    if (!this.props.coordinates) {
      return null;
    }
    const children = [];
    if (this.symbolStyle) {
      children.push(/*#__PURE__*/_jsx(SymbolLayer, {
        id: `${this.props.id}-symbol`,
        style: this.symbolStyle
      }));
    }
    if (this.props.children) {
      if (Array.isArray(this.props.children)) {
        children.push(...this.props.children);
      } else {
        children.push(this.props.children);
      }
    }
    return /*#__PURE__*/_jsx(Animated.ShapeSource, {
      id: this.props.id,
      onPress: this.onPress,
      shape: this.state.shape,
      children: children
    });
  }
}
export default Annotation;
//# sourceMappingURL=Annotation.js.map